# 0002: Contracts

**Status:** Accepted, 2026-10-22. Asked for by #8.

## Context

Everything that crosses a boundary in v2 is a contract: frames, events,
callouts, KV values, NATS micro services and the pit wall's HTTP API
([0001](0001-v2-architecture.md)). The components live in separate
repositories (0001, point 4) and only meet through these contracts, and
agents change code on both sides of them. So the format and its rules decide
how safely a contract can change: a change that would break a consumer has to
fail a PR before any consumer sees it.

The contracts live in their own repository, `contracts`, and ADR 0001 left
three things to this record: the schema format, what goes in a message's
headers, and how a contract is released and consumed.

Five sources of truth were compared with protobuf: JSON Schema, TypeSpec, Go
structs, Avro and CUE. Each was weighed on code generation for Go,
TypeScript and Python, breaking-change checks, how readable it is, how heavy
its toolchain is and how well agents work with it. Two reasons decided it for
protobuf:

- **Breaking-change checks.** `buf breaking` compares a change with the last
  release and fails the PR if it would break a reader. JSON Schema's diff
  tools are best effort, Go structs have none, and TypeSpec's protobuf output
  is still in preview.
- **Code generation in Go, TypeScript and Python.** Protobuf to Go and to
  TypeScript is first class, and Python is served by the official package.
  JSON Schema to Go gives pointers everywhere and odd names; Avro and CUE are
  weak in at least two of the three languages.

Go structs would be the most readable source for the maintainer, but they
have no breaking check and the other two languages come out weak.

## Decision

### 1. Protobuf with `buf` is the source

Every contract is a protobuf message (or service) in `contracts`, under
`proto/twr/<area>/v<N>/`. `buf` is the one tool around them:

- **`buf lint`** with the `STANDARD` rules plus `COMMENTS`, so every message,
  field, enum and value says what it is. The comments are where units and
  meaning live, for agents and for people.
- **`buf format`**, checked in CI with no diff allowed.
- **`buf breaking` in `WIRE_JSON` mode against the last release tag.** The
  wire is JSON (point 2), so a change that keeps the binary encoding but
  changes a JSON name still fails.
- **`buf generate`** with buf's remote plugins, each pinned by version in
  `buf.gen.yaml`, so there is nothing to install and every run produces the
  same code. CI regenerates and fails if `gen/` changes.

`contracts`' `buf.yaml` and CI (#23) carry these settings, and #23's
acceptance criteria are the tests that back them: a renamed field fails
`buf breaking`, a message without a comment fails `buf lint`, and a stale
`gen/` fails the freshness check.

Settles: #23 (`buf.yaml`, `buf.gen.yaml` and the CI steps), #24 and every
later `feat(contracts)` issue (messages written as protobuf).

### 2. JSON on the wire

Messages travel as protobuf's own JSON form (the
[JSON mapping](https://protobuf.dev/programming-guides/json/)), not as
protobuf binary. A message read with `nats sub` or in a JetStream stream is
readable without a decoder, which matters while the system is new and
debugged by hand. At 60 Hz a frame costs a few microseconds to encode in Go;
#26 measures it once.

The encoding is named in each message's `ce-datacontenttype` header (point
4), as `application/json`. A subject can move to binary protobuf later by
changing that value, and a reader that checks it fails clearly instead of
misreading the payload. Moving a subject to binary is still a change every
consumer of that subject has to support first, so it is released like any
other contract change (point 6).

Settles: #24, #26 (the JSON form and the content type), #34 (what `ecu`
publishes).

### 3. The JSON rules

Protobuf's JSON form has sharp edges. These rules avoid them, and hold in
every language:

- **Every reader ignores unknown fields.** By default the parsers in all
  three languages reject a field they do not know, which would break every
  consumer the moment a producer adds a field. Go sets `DiscardUnknown`,
  TypeScript `ignoreUnknownFields`, Python `ignore_unknown_fields`.
- **No 64-bit integers in messages** (`int64`, `uint64`, `sint64`,
  `fixed64`, `sfixed64`). Protobuf's JSON form writes them as strings, which
  readers in JavaScript and hand-written tools get wrong. `int32`, `uint32`
  and `double` are enough; a time in milliseconds as `int32` holds about 24
  days.
- **Field and enum value names are the wire.** Renaming either is a breaking
  change, and `buf breaking` in `WIRE_JSON` mode fails it.
- **`optional` where zero is not the same as absent.** The JSON form leaves
  zero values out, so without `optional` a reader cannot tell "0 laps" from
  "no value".
- **Every enum starts with `<ENUM>_UNSPECIFIED = 0`**, which `buf lint`'s
  `STANDARD` rules check.
- **Tests compare decoded values, never JSON bytes.** Go's encoder varies its
  whitespace on purpose, and field order is not part of the format.
- **One options profile per language, in a helper.** Encoding and decoding go
  through one small package per language in `contracts`, so every component
  uses the same options. Go's is #26; TypeScript's and Python's come with
  their first consumers.

Settles: #24, #26, and every later `feat(contracts)` issue (for example #121,
whose messages follow these rules).

### 4. CloudEvents in binary mode, set by hand

Every event and callout carries [CloudEvents](https://cloudevents.io/)
attributes as NATS headers, in the binding's binary mode: the attributes in
headers, the message in the payload. The headers are:

| Header | Value |
|---|---|
| `ce-specversion` | `1.0` |
| `ce-id` | A deterministic id for this event (below) |
| `ce-type` | `run.thosewho.<area>.<what>.v<N>` |
| `ce-source` | The producer; format still open (below) |
| `ce-time` | When it happened, RFC 3339 in UTC |
| `ce-datacontenttype` | `application/json` (point 2) |
| `Nats-Msg-Id` | The same value as `ce-id` |

- **Set by hand, with a small helper per language** (about 50 lines in Go,
  #26). The CloudEvents SDK's NATS modules bring their own connection
  handling, which fights `nats.go`'s JetStream, KV and micro APIs that the
  components use.
- **`ce-type` is reverse DNS plus a version, one to one with a message:**
  `run.thosewho.lap.completed.v1` is always a `LapCompleted`, and nothing
  else is. A reader checks `ce-type` before it decodes. The mapping lives in
  `subjects.yaml` (point 5) and in the helper, and a test checks they agree
  (#26).
- **`Nats-Msg-Id` equals `ce-id`, and both are deterministic,** built from
  what the event is about, for example `<sid>:lap:<n>:completed`. A replay
  of the same frames or a producer that restarts produces the same id, and
  JetStream's duplicate window drops the second copy, so a stream cannot
  store an event twice (0001, point 1). A replay through `ecu` mints a new
  session id, so it does not collide with the live session it came from. The
  scheme for each event is written in `subjects.yaml` (#25).

Settles: #26 (the helper and the headers), #25 (`ce-type` and the id scheme
per subject), #34 (headers on what `ecu` publishes), #38 (readers that check
`ce-type`).

### 5. `subjects.yaml` instead of AsyncAPI

The subjects are listed in one catalogue in `contracts`, `subjects.yaml`,
with one entry per subject pattern:

- the pattern (for example `twr.v1.<src>.evt.<sid>.lap.completed`);
- its `ce-type`, or none for frames if they carry no CloudEvents headers
  (#24 and #25 decide);
- the protobuf message it carries;
- the stream or KV bucket that captures it, if any (0001, point 1);
- its producer and its consumers;
- for events, the `Nats-Msg-Id` scheme.

A test in `contracts` (#25) checks that every message named exists, that
every `ce-type` is unique and follows point 4, and that every pattern
follows ADR 0001's subject scheme (0001, point 2). The catalogue is rendered
to markdown for reading.

AsyncAPI was the alternative. Its NATS binding has one field, and a protobuf
payload goes in as an opaque string, so it would be hand-written YAML that
repeats the protos and checks nothing. It can be generated from the
catalogue later if anything outside the project ever needs it.

Settles: #25.

### 6. Releases by git tag, no registry

- **The generated code is committed** under `gen/` (`gen/go/` now) and
  released with the protos. A consumer gets code that matches the tag
  exactly, with no generation step of its own.
- **No Buf Schema Registry.** A release is a git tag on `contracts`, made by
  the maintainer when he merges the release PR. Go modules resolve from the
  tag; Python packages can install from it too.
- **Components pin a tag and bump it by PR,** in their own repository, after
  the contract change has merged and been released (see
  [`../../AGENTS.md`](../../AGENTS.md), "Across repositories").
- **Contracts are consumed only by released tag,** never from a branch or an
  untagged commit.
- **A breaking change is a new package version beside the old one, never an
  edit.** A message that has to break moves to a new proto package
  (`twr.events.v2` beside `twr.events.v1`) with a new `ce-type` version
  (`….v2`). Producers publish both, or consumers move first, until nothing
  reads the old one, and only then is it removed. Because the old package
  stays, the Go module itself never needs a new major version for it. The
  `v1` in a subject (0001, point 2) is separate: it changes only if the
  subject scheme itself breaks.
- **The Go module's place and tag scheme** (the module at the repository root
  with plain `vX.Y.Z` tags, or under `gen/go` with path-prefixed
  `gen/go/vX.Y.Z` tags) is still open (Open choices). #23 decides it and
  writes it in `contracts`' README.

Settles: #23 (release tags and the committed `gen/`), #26 (v0.1.0 tagged by
the maintainer), and every consumer's bump issue.

### 7. Evolution

- **Never reuse a field number or a field name.** A field that is removed is
  listed under `reserved`, by number and by name, so neither can come back
  with a different meaning.
- **Never change a field's type.** Add a new field (or a new message) and
  deprecate the old one.
- **Adding a field or an enum value is safe** because every reader ignores
  what it does not know (point 3). A reader that meets an enum value it does
  not know treats it as `…_UNSPECIFIED`.

`buf breaking` in `WIRE_JSON` mode enforces the first two; the third relies
on the helper's options profile (point 3).

Settles: #23 (the rules written in `contracts`' `AGENTS.md`), and every later
`feat(contracts)` issue.

## Open choices

These are not decided by this record. Each is decided in the issue named,
and a later record or that issue's PR writes the result down.

- **`ce-dataschema`:** set it to an absolute link to the proto at its tag
  (for example
  `https://github.com/those-who-run/contracts/blob/<tag>/proto/twr/events/v1/lap.proto`),
  or leave it out and rely on `ce-type`. Until decided, it is left out.
  (#26)
- **`ce-source` format:** for example `/twr/ecu/gt7` (component and game).
  (#26)
- **The Go module's place and tag scheme:** the root module with plain
  `vX.Y.Z` tags, or a `gen/go` module with `gen/go/vX.Y.Z` tags. (#23)
- **The pit wall's HTTP API:** Connect, generated from the same protos, is
  the recommended direction, with GraphQL parked as a possible experiment.
  Rosberg specifies it (#121).
- **Where TypeScript packages are published:** npmjs.com with trusted
  publishing is recommended, since GitHub Packages' npm registry needs a
  token even to install a public package. Not needed until a TypeScript
  consumer exists (`pitwall`, `dash`).

## What #8 does because of this

#8 is this record and its row in [`README.md`](README.md). The `contracts`
issues (#23 to #26) refer to it for their format, rules and release scheme,
and `contracts`' `buf.yaml` (#23) is checked against point 1 when it lands.

## Consequences

- **Go now, TypeScript and Python when their first consumer needs them.**
  `contracts` v0.1 generates Go only. TypeScript (protobuf-es) comes with
  `pitwall` and `dash`, and Python (the official `protobuf` package) with
  `radio`, each with its options-profile helper. Each adds a plugin to
  `buf.gen.yaml` and a directory under `gen/`.
- **`buf breaking` is the guardrail that matters most** when agents change
  contracts. It runs on every PR in `contracts`, and loosening it needs the
  maintainer's review like any other change to a check.
- **`WIRE_JSON` does not catch source-level breaks,** such as a renamed
  generated Go package or a moved file. A consumer finds those when it bumps
  the tag and its build fails, which is in its own PR and before anything is
  deployed.
- **JSON costs size.** A frame as JSON is about 1.3 KB (0001, point 1), more
  than binary protobuf would be. Point 2 keeps the way to binary open per
  subject.
- **More ceremony per change.** A field needs a comment, deletes need
  `reserved`, a type change needs a new field, and a consumer waits for a
  release and a bump. That is the price of changes that fail early.
- **Two places name each `ce-type`:** the catalogue and the helper. #26's
  test keeps them in step.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **JSON Schema, TypeSpec, Go structs, Avro, CUE** as the source (Context).
- **Binary protobuf on the wire:** smaller and faster, but unreadable with
  `nats sub`. Kept possible per subject (point 2).
- **The CloudEvents SDKs:** their NATS transports own the connection, which
  clashes with JetStream, KV and micro (point 4).
- **AsyncAPI** for the subjects (point 5).
- **The Buf Schema Registry:** generated SDKs on demand, but another service
  and account, and nothing a git tag with committed code does not already
  give a project this size (point 6).

## References

- Protobuf JSON mapping: https://protobuf.dev/programming-guides/json/
- buf lint rules: https://buf.build/docs/lint/rules/
- buf breaking rules: https://buf.build/docs/breaking/rules/
- CloudEvents NATS binding:
  https://github.com/cloudevents/spec/blob/main/cloudevents/bindings/nats-protocol-binding.md
- NATS message headers, from `nats-server` 2.2:
  https://github.com/nats-io/nats-architecture-and-design/blob/main/adr/ADR-4.md
- JetStream message deduplication:
  https://docs.nats.io/using-nats/developer/develop_jetstream/model_deep_dive
- AsyncAPI NATS bindings: https://www.asyncapi.com/docs/reference/bindings/nats
- those-who-run/those-who-run#386 (how v1's web app and server would share
  contracts; superseded by 0001 and this record)
