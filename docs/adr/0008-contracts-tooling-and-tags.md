# 0008: Contracts' tooling and tags, local plugins and a root module

**Status:** Accepted, 2026-10-24. Asked for by #23. Supersedes
[ADR 0002](0002-contracts.md), points 1 (how `buf generate` gets its plugin) and
6 (the Go module's place and tag scheme).

## Context

[ADR 0002](0002-contracts.md) made protobuf with `buf` the source of every
contract. Two of its details are settled differently, or were left open:

- Point 1 has `buf generate` use buf's remote plugins, pinned by version in
  `buf.gen.yaml`. That runs `protoc-gen-go` on buf.build, so every generation
  is a network call to a service outside the project. Since then the Go
  template (#21) has pinned its tools in a separate `tools/go.mod` and run
  them with `go tool`, so `make check` and CI run the same binaries.
- Point 6 left the Go module's place and tag scheme to #23: the module at the
  repository root with plain `vX.Y.Z` tags, or a module in `gen/go` with
  `gen/go/vX.Y.Z` tags.

#23 sets up `contracts` and needs both. The maintainer settled them in the
triage of #23 on 2026-10-23
([the decision](https://github.com/those-who-run/paddock/issues/23#issuecomment-6367527236)).
An accepted record is not edited, so this record carries those choices.

## Decision

### 1. `buf` and `protoc-gen-go` are tools in `tools/go.mod`, run with `go tool`

`contracts` pins `buf` (`github.com/bufbuild/buf/cmd/buf`) and `protoc-gen-go`
(`google.golang.org/protobuf/cmd/protoc-gen-go`) in its `tools/go.mod`, beside
golangci-lint and govulncheck, as the template does (#21). `make` runs them
with `go tool -modfile=tools/go.mod …`, and `buf.gen.yaml` names
`protoc-gen-go` as a local plugin, not a remote one.

- Generating needs no network call to buf.build, and a laptop and CI produce
  the same code from the same versions.
- Renovate updates both, through the weekly PR, like any Go tool.
- No `bufbuild/buf-action` is needed, so the organisation's Actions allowlist
  doesn't change.

The rest of ADR 0002 point 1 stands: `buf lint` with `STANDARD` plus
`COMMENTS`, `buf format` with no diff, `buf breaking` in `WIRE_JSON` mode
against the last release tag, and a CI check that `gen/` is fresh.

Settles: #23 R3 and R8, and the first bullet of its Approach.

### 2. The Go module at the repository root, with plain `vX.Y.Z` tags

The Go module is `github.com/those-who-run/contracts`, at the repository root.
Generated Go code goes under `gen/go/…`, inside that module, and a release is
a plain `vX.Y.Z` tag.

- A consumer pins it as any Go module:
  `go get github.com/those-who-run/contracts@v0.1.0`, and imports the packages
  under `gen/go/`.
- release-please needs no component, and the release tag pattern is the
  default `v*`, which `contracts`' tag ruleset and its guard already protect.
- The helpers (#26) live in the same module, so the test that `subjects.yaml`
  and the helper agree (ADR 0002, point 4) stays inside one module.

A Go module in `gen/go` would have kept the protos and generated code apart and
left room for TypeScript and Python to version separately. Their generated code
can still live under `gen/` in the same repository; only Go reads the module.

Settles: #23 R5 and R6, #26 (where the helpers live), and ADR 0002's open
choice on the Go module's place and tag scheme.

## What #23 does because of this

#23 builds `contracts` on these two points and writes the module path, the tag
scheme and how a consumer pins it into `contracts`' README (its fourth
acceptance criterion). This record and its row in [`README.md`](README.md) mark
the decision; ADR 0002's status line and its index row say "Superseded in part
by 0008 (points 1 and 6)", as the [process](README.md#how) describes. The rest
of ADR 0002 still holds.

## Consequences

- **`tools/go.mod` grows.** `buf` brings a large dependency tree into it. Each
  module's licence goes through dependency review like any other.
- **`protoc-gen-go` follows `buf`'s protobuf version.** Both are tools in one
  module, so `protoc-gen-go` runs at the `google.golang.org/protobuf` version
  that `buf` requires, and they move together in the weekly PR.
- **Generated code counts neither way for the coverage floor.** From
  `those-who-run/.github`'s v0.2.4, go.yml leaves files with Go's
  `// Code generated … DO NOT EDIT.` line out of the total, so the floor
  measures the code people and agents write.
- **One version for everything in the module.** The helpers and the generated
  code are released together, under one tag.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **buf's remote plugins**, as ADR 0002 point 1 had it: nothing to install, but
  a network call to buf.build on every generation.
- **`bufbuild/buf-action` in CI**: an addition to the Actions allowlist, and a
  `buf` that `make check` on a laptop would have to install another way.
- **A Go module in `gen/go` with `gen/go/vX.Y.Z` tags**: protos and generated
  code kept apart, at the cost of a release-please component, prefixed tags
  for every consumer, and the helpers in another module from the catalogue
  they are tested against.

## References

- [ADR 0002](0002-contracts.md), points 1 and 6.
- #23, and the maintainer's triage decision on it.
- Go modules in subdirectories: https://go.dev/ref/mod#vcs-version
- Generated Go files: https://go.dev/s/generatedcode
