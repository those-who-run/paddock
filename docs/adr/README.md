# Decision records

An ADR (architecture decision record) writes down one decision, why it was
made and what follows from it, so the issues that depend on it stop deciding
it again, each a little differently.

v2's records live here, in `paddock`, and nowhere else: the component
repositories hold code, not decisions. The process is v1's, carried over.

## When to write one

Write an ADR when **more than one issue depends on the same decision**: how
events travel, who owns the data volume, how a component's repository is
laid out. A decision that only one issue needs stays in that issue (its
Approach or Open questions).

Signs it is time:

- two specs recommend different answers to the same question;
- a spec's Open questions say "decide in #…" or "record it somewhere";
- a requirement in one issue breaks a requirement in another.

## How

- One file per record: `NNNN-short-name.md`, numbered in order. A number is
  never reused, not even for a record that was dropped.
- Sections: **Status**, **Context**, **Decision** (one heading per point, each
  naming the issues it settles), **Consequences**, and what it means for the
  issue that asked for it.
- Status is *Accepted* or *Superseded by NNNN*. A record is not edited to say
  something new: a new record replaces it, and the old one points to it.
- A later record may supersede a single point of an earlier one. The earlier
  record's status line and its index row then say "Superseded in part by
  NNNN (point N)".
- Every issue a record settles links back to it, and its body agrees with it.
  Where a test can back a decision, the record names the test.
- Plain and short, like the rest of the docs (see
  [`../ISSUE_FORMAT.md`](../ISSUE_FORMAT.md), "Writing").

## Records

| ADR | Decision |
|---|---|
| [0001](0001-v2-architecture.md) | The v2 architecture: events over NATS (design B), the subject scheme, who owns the session and the rules, one repository per component, the languages, the dash reading NATS directly, replays and parity. Superseded in part by [0006](0006-images-on-ghcr.md) (point 4). |
| [0002](0002-contracts.md) | Contracts: protobuf with `buf` as the source, protobuf's JSON form on the wire, the JSON rules, CloudEvents headers set by hand, the `subjects.yaml` catalogue, releases by git tag with generated code committed, and how a contract evolves. Superseded in part by [0008](0008-contracts-tooling-and-tags.md): how `buf generate` gets its plugin (point 1) and the Go module's place and tag scheme (point 6). |
| [0003](0003-the-store.md) | The store: `logbook` owns its data, the only writer and reader of the volume, serving everything else through services; one writer carried from v1; how it subscribes and writes idempotently; DuckDB as the lean for the engine (open). Superseded in part by [0007](0007-the-store-engine.md) (points 6 and 7). |
| [0004](0004-beside-v1.md) | v2 runs beside v1: one engineer live at a time, the switch (`engineer: v1 \| v2`) in the deployment rather than in v1, the idle gate on both sides, the `twr` namespace and its own Argo CD application, a second Discord bot, v1's history kept as an archive, kustomize with Helm only for third-party charts, and the swap. Superseded in part by [0009](0009-v1-history-import.md) (point 7). |
| [0005](0005-milestone-names.md) | Milestones are named after F1 drivers: `ROADMAP.md` alone holds their order, they are not versions, they live in `paddock`, the phase is a label, and a name is never reused. |
| [0006](0006-images-on-ghcr.md) | v2 images on GitHub's container registry, `ghcr.io/those-who-run/twr-<repo>:X.Y.Z`, pushed with the publishing job's own `GITHUB_TOKEN`, amd64 only; v1 stays on Docker Hub. Supersedes ADR 0001, point 4, on images. |
| [0007](0007-the-store-engine.md) | The store's engine: DuckDB embedded in `logbook` through the official Go driver, writing the Parquet lap files itself, with cgo and glibc confined to `logbook`'s image; one engine, the index in `index.duckdb`. Supersedes ADR 0003, points 6 and 7. |
| [0008](0008-contracts-tooling-and-tags.md) | Contracts' tooling and tags: `buf` and `protoc-gen-go` pinned in `tools/go.mod` and run with `go tool`, `protoc-gen-go` as a local plugin; the Go module at the repository root, `github.com/those-who-run/contracts`, with plain `vX.Y.Z` tags. Supersedes in part ADR 0002: how `buf generate` gets its plugin (point 1) and the Go module's place and tag scheme (point 6). |
| [0009](0009-v1-history-import.md) | v1's history is imported into `logbook`, re-derived from v1's stored frames by `logbook import-v1` through `ecu`'s race packages, made public, as a library; imported sessions are `src=v1-import` and keep v1's ids, so old card links work; sessions without the frames v2 needs come from v1's rows, flagged. Supersedes ADR 0004, point 7. |
| [0010](0010-results-cards.md) | Results cards are posted by `podium`, their own subscriber in Go (`ghcr.io/those-who-run/twr-podium`, `component: podium`), over Discord's REST API with the second bot's token and no gateway session; `podium` is the home for everything posted after a session. |
| [0011](0011-pit-wall-access.md) | Who can reach the pit wall: the home network only, with no login, as v1; from outside through a VPN, later; the v2 pit wall gets a new host, then takes v1's address once the import is checked, and v1's pit wall retires; both addresses serve it for good, as cards link to each; Schumacher's setup pages revisit it. |

## v1's records

v1's records stay in v1's repository and are linked here, not copied, so
there is one copy of each. Each row says what still holds in v2, and which v2
record carries it on or replaces it.

| v1 ADR | What still holds in v2 |
|---|---|
| [0001, data volume and network](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0001-data-and-network.md) | The one writer on the data volume carries into ADR 0003 (#9). The rest (ports, secrets, images, Postgres) is v1's. |
| [0002, the outing archive](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0002-outing-archive.md) | Still where archived recordings live. v2's fixtures come from it. |
| [0003, UI stack](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0003-ui-stack.md) | One origin holds for the pit wall. The dash is its own app with a second origin, NATS ([0001](0001-v2-architecture.md), point 6). |
| [0004, design language](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0004-design-language.md) | Holds for `pitwall` and `dash` as written. |
