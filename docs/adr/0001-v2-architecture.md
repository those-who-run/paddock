# 0001: The v2 architecture

**Status:** Accepted, 2026-10-22. Asked for by #7. Superseded in part by
[0006](0006-images-on-ghcr.md) (point 4).

## Context

v1 ([those-who-run/those-who-run](https://github.com/those-who-run/those-who-run))
is a voice race engineer for Gran Turismo 7, with a session store and a pit
wall. It works, and from 1.0.0 it is in maintenance. What it cannot do well is
grow. A review of v1's open issues on 2026-10-17 found its engine, its store
and its entry point each touched by 20 to 28 of them: almost every new feature
had to change the same few modules. Every moment the engine detects is a
direct call into the store, the rules, the voice queue or the metrics, and
none is published for anything else to use. The cost showed in
those-who-run/those-who-run#340: voice, the store and telemetry live in one
process, so they fail together, and the engineer went quiet mid-race.

v2 rebuilds the same product from a clean slate, with what v1 taught. The
project is self-hosted and has no customers, so it is also a place to learn:
a message bus (NATS), more than one language, and agents doing most of the
work across several repositories while the maintainer reviews. Parity with
v1, checked by replaying the same recordings through both, comes before any
new feature.

Every component issue depends on the same few decisions: how frames and
events travel, who publishes what, and where a component's code lives. This
record makes them once. The contracts' format (ADR 0002, #8), the
store (ADR 0003, #9), running beside v1 (ADR 0004, #10) and the milestone
names (ADR 0005, #11) are separate records.

One rule from v1 shapes all of it. v1 learnt the hard way that nothing slow
may run on the loop that handles frames (a long lap once blocked the Discord
heartbeat for over a minute; see those-who-run/those-who-run#196). In v2 that
becomes: **the frame path never waits on the network.**

## Decision

### 1. Events over NATS, design B

The components talk over one NATS server. Three designs were weighed:

- **A.** Core NATS only, fire and forget, with KV for live state. A store or
  voice service that restarts loses what it missed.
- **B.** Producers do a plain core publish; JetStream streams capture the
  subjects that matter. Chosen.
- **C.** JetStream for everything, every publish acknowledged. This puts an
  acknowledgement on the frame path, duplicates the store's job, and gives
  phones a 60 Hz durable consumer.

Under design B:

- **Producers only ever do a plain core publish.** A JetStream stream
  captures every message on a matching subject, whoever published it and
  however, so the producer never waits for an acknowledgement and the
  consumers still get messages that survive a restart.
- **Streams:**
  - `EVENTS`: domain events (`twr.v1.*.evt.>`). Consumers such as the store
    read with durable consumers and acknowledge after their own write
    commits.
  - `CALLOUTS`: callouts (`twr.v1.*.callout.>`), with work-queue retention.
    Voice is the one consumer.
  - `TELEMETRY`: full-rate frames (`twr.v1.*.frame.*`), kept as a flight
    recorder with a short `max_age`, so a consumer that restarts mid-lap can
    pick the lap up again instead of losing it. The single `*` matches
    `frame.gt7` and any other game's frame subject, but not the 20 Hz copy
    (`frame.gt7.lo`), which is derived from the same frames and not kept.
    The store's own files stay the record of what happened; `TELEMETRY` is
    not.
- **KV buckets:**
  - `live`: the current state (the open session, pit status, telemetry age,
    each service's health), so a client that joins late reads the current
    value at once and then watches it.
  - `config`: settings, which services watch and reload.
- **Deduplication.** Events carry a deterministic `Nats-Msg-Id` (for
  example `<sid>:lap:<n>:completed`), so a replay or a restarted producer
  cannot store the same event twice. The scheme for each event is in
  `contracts` (#25).

Starting values, which the deploy issues (#36, #43) may tune without a new
record: `TELEMETRY` on file storage with `max_age` 2 h and old messages
discarded first; `EVENTS` with `max_age` 30 d and a 2 min duplicate window;
`CALLOUTS` with `max_age` 15 s. At 60 Hz a frame as JSON is about 1.3 KB,
which is about 80 KB/s, or about 280 MB per hour of driving in `TELEMETRY`
(the 60 Hz frames only; the 20 Hz copy is not captured).
These are estimates from v1's frame, to be measured.

Settles: #34 (core publish of frames and events), #36 and #43 (the streams
and buckets to create), #38 (durable consumers that acknowledge after
commit), #56 (callouts from a work queue).

### 2. Subjects

```
twr.v1.<src>.frame.gt7                    frames, 60 Hz
twr.v1.<src>.frame.gt7.lo                 frames, 20 Hz, for phones
twr.v1.<src>.evt.<sid>.<area>.<what>      events, e.g. lap.completed
twr.v1.<src>.callout.<p0|p1|p2>           callouts, by priority
svc.<service>.<verb>                      NATS micro services, e.g. svc.ecu.status
```

- **Prefix `twr`** (Those Who Run), the product rather than the game.
- **The major version** (`v1`) is in the subject and changes only for a
  breaking change. Everything else about a message (its type, schema and
  content type) goes in headers; ADR 0002 (#8) decides those.
- **`<src>`** is `live`, `replay-<run>` or `parity`. Live driving, replays
  and parity runs share one server without mixing, and a checker can watch
  `twr.v1.*.evt.>` across all of them.
- **The game is on frame subjects** (`frame.gt7`), because a frame's fields
  are the game's own. A consumer for GT7 only subscribes to `…frame.gt7`, and
  a game-neutral one to `…frame.*` (full rate, every game). Adding the token
  later would change every subject, which is a breaking change for every
  subscriber; adding it now costs nothing.
- **Events are game-neutral** (a lap completed is a lap completed) and carry
  the session id, so a consumer can follow, purge or replay one session. The
  game is named in the event's headers and in `session.started`, not in the
  subject.
- **`frame.gt7.lo`** is the same frame at 20 Hz. A slow consumer on core
  NATS is cut off, so phones on Wi-Fi never subscribe to 60 Hz.
- **Callouts carry their priority** in the subject, so voice takes `p0`
  before `p1` and `p2`.
- **Control and queries** are NATS micro services (`svc.<service>.<verb>`),
  which give discovery and statistics without extra code. HTTP stays for
  browsers.

The full list of subjects, with the message on each, is `contracts`'
`subjects.yaml` (#25), and it grows milestone by milestone. Its test (#25,
R3) checks the `twr.v1.` patterns against this scheme. The
`svc.<service>.<verb>` subjects sit outside that check: they carry requests
and replies, not published messages, and #25 leaves services out of the
catalogue until their messages exist. When they are added, the check has to
accept the `svc.` prefix as well.

Settles: #25, #34, #71 (callouts by priority), #88 (`svc.ecu.status`), #138
(the 20 Hz frame).

### 3. Who owns what

- **`ecu` owns the session.** It mints the session id, decides when a
  session starts, continues and ends, and keeps the open session in KV. After
  a restart it decides whether to continue from KV, with no request to the
  store. In v1 the store minted the id and the engine read it back; that
  coupling goes.
- **Built-in rules stay in `ecu`** (fuel, laps, position, temperatures, the
  start brief, the summary). They need per-frame state and a fixed order
  within a frame (pit status before fuel, before the lap crossing), and they
  are what parity compares. Across the bus every event carries the frame
  sequence number it happened on, and consumers order by that, not by arrival.
- **New rules are subscribers.** Anything that publishes a callout to
  `twr.v1.<src>.callout.*` gets spoken: a coach, a strategy experiment, a
  rule in another language. This is how v2 is extended without touching
  `ecu`. Where such rules live is a later record (#150).
- **Every feature is a subscriber and fails alone.** The store, voice, the
  results card, the pit wall's live status and the dash each subscribe to
  what they need. `ecu` knows nothing about who listens, and a subscriber
  that crashes takes nothing else with it.

Settles: #33, #76 (continue after a restart, from KV), #89 (the rule engine
in `ecu`).

### 4. One repository per component

Every component is its own repository in the `those-who-run` organisation,
with its own CI, releases and image. A repository boundary cannot be crossed
with a quiet import, which is the tangle v2 exists to avoid, and each piece
releases on its own.

| Repository | What | Language | Ships as |
|---|---|---|---|
| `paddock` | Every v2 issue, the milestones, the roadmap, the ADRs, and the deployment base. No service code | YAML | |
| `contracts` | The messages, `subjects.yaml`, generated code (ADR 0002, #8) | Protobuf | A Go module, and packages for TypeScript and Python, by tag |
| `salsa` | The GT7 decoder: datagrams in, typed packets out | Go | A Go module |
| `ecu` | Ingest, race state, the built-in rules; publishes frames, events and callouts | Go | `muldoon/twr-ecu` |
| `logbook` | The store: owns the data volume, writes laps and sessions, answers queries (ADR 0003, #9) | Go | `muldoon/twr-logbook` |
| `radio` | Voice: callouts to speech in Discord, the pit-lane policy | Python | `muldoon/twr-radio` |
| `pitwall` | The pit wall: the web app and its API over `logbook`, in one repository | TypeScript and Go | `muldoon/twr-pitwall` |
| `dash` | The live dashboard, reading NATS directly | TypeScript | `muldoon/twr-dash` |
| `shakedown-<name>` | Experiments, one repository each | Any | Whatever it needs |

Plus `.github`, which holds the organisation's community files and the
reusable workflows every repository calls.

- **Images** are `muldoon/twr-<repo>`.
- **Issues, milestones and ADRs live only in `paddock`.** A PR in a
  component repository closes its issue with `Closes those-who-run/paddock#N.`
  (see [`../ISSUE_FORMAT.md`](../ISSUE_FORMAT.md)).
- **Experiments** are `shakedown-<name>`. One that works gets a proper name
  and its own row here in a later record; one that does not is archived, and
  nothing else notices.
- **Deployment** is a public base in `paddock` with a private overlay: the
  base is portable to any cluster, and everything about a particular
  installation (hosts, addresses, secrets) lives in an overlay kept outside
  this repository.
- Where results cards are posted from is a later record (#65).

Settles: those-who-run/those-who-run#347 and those-who-run/those-who-run#386
(the web app in its own repository, with the contracts made explicit), which
this decision supersedes; #29, #44, #47, #53.

### 5. Languages

- **Go by default for services:** `salsa`, `ecu`, `logbook` and the pit
  wall's API. Agents write most of the code and the maintainer reviews it, so
  the language that matters is one he reads easily. Go also gives one static
  binary, NATS's reference client, and a toolchain that suits guardrails
  (`gofmt`, `go vet`, `go test -race`, `internal/` packages for import
  boundaries).
- **Python for `radio`**, because Piper and Discord voice force it: Discord
  voice support in Go is weak.
- **TypeScript in the browser:** the pit wall's web app and the dash.
- **Experiments** may use any language; being a subscriber is enough.

Every v1 rule is rewritten in Go, not lifted. Parity (point 7) is what makes
that safe.

Settles: #21 and #22 (the Go template), #55 (`radio` in Python).

### 6. The dash reads NATS directly

- **The dash is its own app,** served as static files from its own image. It
  needs nothing from the pit wall, so it keeps working when the pit wall is
  down.
- **It connects to NATS over WebSocket** as a read-only user allowed only
  the low-rate frame subject (`twr.v1.live.frame.gt7.lo`) and the `live` KV,
  plus the reply and KV API subjects a watch needs. That user's credentials
  ship in the dash's bundle, which is acceptable on a private network only.
  If the dash is ever reachable from outside it, this decision is revisited
  first.
- **What the browser may read is a contract,** written in `contracts` like
  any other (#137).
- **NATS gets its own HTTPS name** for WebSocket, because the dash needs a
  secure context to install and to keep the screen on (#142).
- **The pit wall keeps one origin,** as in v1's ADR 0003: its history and
  queries go through its own API, and it holds no NATS credentials in the
  browser. The dash has two origins, its own and NATS.

Settles: #137, #140, #142, #42 (NATS's WebSocket on its own HTTPS host).

### 7. Replays and parity

- **A replayer publishes recorded frames** under `twr.v1.replay-<run>.…`, at
  recorded speed or faster, and drives the real `ecu` and every subscriber
  end to end. JetStream's own replay only re-paces a stream to one consumer,
  so it is not a frame source.
- **Parity** is the same recording through v1 and v2, with the events (and
  later the callouts) normalised and compared. A v2 piece goes live only when
  it matches v1 on recordings.
- The race state and rules in `ecu` are a library driven by frames, tested
  with no NATS at all. NATS is an adapter at the edge, and integration tests
  start a real server for each run.

Settles: #35 (replay onto the bus), #40 and #104 (the parity harness), #45.

## What #7 does because of this

#7 is this record and the README beside it. Each later record (#8 to #11)
adds its row to the README's index. Issues that use a subject, stream or
bucket name refer to this record for it, and `contracts`' `subjects.yaml`
(#25) is checked against point 2 when it lands.

## Consequences

What this costs:

- **Two delivery models to learn.** Core NATS drops messages for a slow or
  absent subscriber; JetStream keeps them. Each subject needs a deliberate
  choice of which applies, and a consumer has to know which it is reading.
- **Stream and bucket configuration lives in git**, applied by an
  idempotent bootstrap (#36, #43). Streams without limits fill the disk, and
  two streams may not capture overlapping subjects, so every new subject is
  checked against the existing streams.
- **Two server languages,** Go and Python, each with its own template, CI and
  dependency updates. Python is kept to one service with a narrow contract.
- **More repositories,** each with its own CI and releases. A change across
  repositories (a contract and its consumers) lands in order: the contract
  first, then each consumer's bump.
- **The dash depends on the bus's subjects and payloads,** not on an API the
  pit wall can reshape. Point 6 keeps that surface to one subject and one
  bucket.

Hard constraints carried from v1:

- **One UDP listener per PS5.** Only one process receives a console's
  telemetry. While v1 and v2 run side by side, ADR 0004 (#10) decides which
  one listens.
- **One Discord bot per token.** Two processes cannot speak as the same bot,
  so v2's `radio` uses a second bot with its own token while v1 keeps its
  own.
- **One writer on the data volume.** `logbook` is that writer (ADR 0003,
  #9); `ecu` writes nothing to disk and keeps its state in KV.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **Plain HTTP and WebSocket between services, no broker.** v1's planned
  split into a web app and a server tied by an HTTP API
  (those-who-run/those-who-run#347, those-who-run/those-who-run#386) leaned
  this way. It leaves each producer knowing its consumers, which is the
  coupling v2 removes.
- **Designs A and C** (point 1).
- **The dash through the pit wall,** either fanned out by the pit wall's API
  or routed through its origin. Either keeps one origin, but the dash then
  goes down with the pit wall.
- **One repository with several services.** Cheaper to run, but imports
  cross freely, which is how v1's tangle grew.

## References

- v1's ADRs: https://github.com/those-who-run/those-who-run/tree/main/docs/adr
- those-who-run/those-who-run#340, those-who-run/those-who-run#196,
  those-who-run/those-who-run#386, those-who-run/those-who-run#347
- NATS JetStream: https://docs.nats.io/nats-concepts/jetstream
- NATS subjects: https://docs.nats.io/nats-concepts/subjects
- NATS key-value store: https://docs.nats.io/nats-concepts/jetstream/key-value-store
- NATS services (micro): https://docs.nats.io/using-nats/developer/services
- NATS WebSocket: https://docs.nats.io/running-a-nats-service/configuration/websocket
