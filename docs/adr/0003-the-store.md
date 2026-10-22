# 0003: The store, logbook owns its data

**Status:** Accepted, 2026-10-22. Asked for by #9. Two choices inside it are
still open and marked **Open** (points 6 and 7). If the maintainer settles
them before this merges, the points say what was chosen. After it merges, a
later record settles each one, and this status line then says "Superseded in
part by NNNN (point N)", as the [process](README.md#how) describes.

## Context

In v1 the engineer writes the session data (a SQLite index and a Parquet file
per lap) and the pit wall reads the same files straight off the shared
volume. v1's [ADR 0001](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0001-data-and-network.md)
made that safe: one writer holding a lock, readers opening the index with
`mode=ro`, and a second read-only mount for the lap files. It works, at two
costs:

- **A hidden contract.** The pit wall depends on the index's tables and the
  Parquet columns. Neither is written down anywhere as a contract, so a change
  to the store is a change to the pit wall, found out at run time.
- **Two pods tied to one node.** The volume is `ReadWriteOnce` and pinned to
  the node that holds it, so every pod that reads it has to run there too.

There is a third reason the files are split the way they are. A DuckDB file
allows one read-write process or many read-only ones, never a reader beside a
writer ([DuckDB concurrency](https://duckdb.org/docs/current/connect/concurrency.html)).
That is why v1 keeps a SQLite index beside the Parquet files, and uses DuckDB
only to write and read the laps.

In v2 the store is `logbook`, a subscriber like every other feature
([ADR 0001](0001-v2-architecture.md), point 3). Brabham's `logbook` issues
and Rosberg's pit wall both depend on who owns the data, so this record
decides it once.

## Decision

### 1. logbook owns the data

Decided by the maintainer: `logbook` owns all reads and all writes. It is the
only process that writes the data volume and the only one that reads it. No
other component shares its state, its files or its locks, and nothing else
mounts the volume: not the pit wall, not an experiment, not a job, not a
command-line tool.

Everything else goes through `logbook`'s services:

- **NATS micro services**, named `svc.logbook.<verb>` under ADR 0001's
  pattern (point 2), for experiments and any other service.
- **The pit wall's API** serves the browser over Connect, from its own origin
  ([ADR 0002](0002-contracts.md); ADR 0001, point 6), and gets its data from
  `logbook`'s services. Whether `logbook` serves it over NATS micro or over
  Connect is #122's to decide.

The services are defined in `contracts` when they are first needed, in the
format [ADR 0002](0002-contracts.md) (#8) sets.

Settles: #9 R1.1 and the issue's first open question (the record's title);
#122 and #124 (the pit wall reads only through `logbook`).

### 2. The volume is not a contract

The files' layout, their names, the index's tables and the Parquet columns
are `logbook`'s own business. They can change in any `logbook` release
without any other component knowing. Only `logbook`'s services are contracts,
and they live in `contracts` and change by its rules
([ADR 0002](0002-contracts.md), #8).

A consumer that wants a new query asks for a new service or a new field in
`contracts`; it never reads the files.

Settles: #9 R1.2; #122 (the pit wall never touches the store's files).

### 3. One writer, carried from v1

v1's [ADR 0001, point 1](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0001-data-and-network.md#1-one-writer-the-engineer)
carries over: one process writes the data volume, and it holds the volume's
lock for as long as it runs. In v2 that process is `logbook`.

- `logbook` takes an exclusive, non-blocking `flock` on a lock file at the
  root of the data directory when it starts, and refuses to start if another
  process holds it.
- It runs as a single replica, replaced with the `Recreate` strategy, so an
  upgrade never runs two `logbook` pods against one volume. The lock is the
  guard if that slips.
- Anything that has to change the data (a backfill, a prune, a re-run) is a
  `logbook` service call or runs inside `logbook`, never a second process
  with the volume mounted. `logbook`'s own commands (such as
  `logbook export`, #37) take the same lock, so they refuse while `logbook`
  runs.

Settles: #9 R1.3; #37 R1 (the lock). Replaces v1's readers rule (v1 ADR 0001, point 2) for v2:
there are no other readers.

### 4. How logbook subscribes

The streams are ADR 0001's (point 1).

- **Frames** come from the `TELEMETRY` stream. `logbook` buffers a lap's
  frames in memory, as v1 does. After a restart, `logbook` resumes from the
  first frame of the lap in progress, so no committed lap is read twice and
  no lap is lost. `TELEMETRY`'s `max_age` (2 h to start) is the longest
  `logbook` can be down without losing frames.
- **Events** come from the `EVENTS` stream with a durable consumer.
  `logbook` acknowledges an event only after the write it causes has
  committed, so an event whose write failed or was cut short is delivered
  again.
- **Order.** Frames and events arrive on separate streams. `logbook` cuts a
  lap's frames by the frame sequence number the event carries (ADR 0001,
  point 3), not by arrival, and writes a lap only once it holds every frame up
  to that number.

What is decided here is the guarantee: a restart resumes the lap in progress,
and events are acknowledged after commit. How the consumers are set up is
#38's (R1, the durable consumers).

Settles: #9 R1.4; #38.

### 5. Idempotent writes

Redelivery is normal under point 4, so every write is safe to repeat:

- **The lap file before its index row,** v1's rule from
  those-who-run/those-who-run#148: a reader that finds the row always finds
  the file.
- **Fixed paths, written to a temporary path and renamed** over the final
  one, so a crash leaves the old file or the new one, never half of one.
- **Rows upserted** on the session id, and on the session id and lap number,
  so writing the same lap twice leaves one row.
- Upstream, events carry a deterministic `Nats-Msg-Id` (ADR 0001, point 1),
  so `EVENTS` does not store the same event twice in the first place.

Tests in `logbook` back this point (Brabham): the same lap delivered twice
leaves one file and one row, and a crash between the file and the row leaves
the file and no row, which the next delivery completes.

Settles: #9 R1.5; #37 R6 and #38 R6 (writes safe to repeat).

### 6. The engine: DuckDB is the lean (Open)

**Open.** The maintainer has not chosen yet. The lean is **DuckDB embedded
in `logbook`**, writing the Parquet lap files itself (`COPY … TO`), using the
official Go driver. The fallback is **SQLite plus Parquet in pure Go**.

| | DuckDB embedded | SQLite plus Parquet, pure Go |
|---|---|---|
| Build | Needs cgo and a glibc base image (no Alpine); a larger binary, about 30 MB more | `CGO_ENABLED=0`, one static binary |
| Lap files | DuckDB writes Parquet itself | A Parquet library in Go |
| Queries | SQL over the Parquet files: thinning a trace with window functions (as v1 does), lap against lap, every session at a track, corner statistics | The index in SQL; traces read and thinned in Go code |
| Features after parity | The debrief, corner coaching, personal bests and progress are analytics over laps, which is what DuckDB is for | Each one is more Go code, or a new tool |

Why the lean: the features after parity are mostly analytics over laps, and
v1 already relies on DuckDB for its traces. The cost is cgo in one image.

Switching later, either way, changes no contract: nobody else sees the files
(point 2). A switch is a `logbook` release that migrates its own data.

Settles, once chosen: #9 R1.6 and the issue's second open question; #37's
engine (its Approach follows the lean).

### 7. One engine or two (Open)

**Open.** If DuckDB is chosen, the index (sessions and laps) can live in
DuckDB too, one engine with one file, since point 1 removes the reason v1
kept SQLite beside it. Or `logbook` can keep a SQLite index beside the
Parquet files, as v1 does, so the index does not depend on DuckDB's file
format between releases. If SQLite plus Parquet is chosen, this question goes
away.

Like point 6, this changes no contract.

Settles, once chosen: #9's third open question; #37's first open question.

## Not decided here

- **After-session jobs** (results cards, the debrief, exports): inside
  `logbook` on a job runner, as v1's those-who-run/those-who-run#189 did, or
  their own subscriber reading through `logbook`'s services. Point 1 rules
  out only a job that mounts the volume. Where results cards are posted from
  is a later record (#65, ADR 0001 point 4); this question goes there, or to
  Button's results card issues. This is the issue's fourth open question.
- **v1's history**: left as an archive for now (ADR 0004, #10).
- **`logbook`'s implementation and service definitions**: Brabham, and
  `contracts` when the services are needed.

## What #9 does because of this

#9 is this record and its row in [`README.md`](README.md). Brabham's
`logbook` issues and Rosberg's pit wall API issues link here: the pit wall
API never mounts the volume and reads only through `logbook`'s services. If
the maintainer settles point 6 or 7 before this merges, the point loses its
**Open** mark and says what was chosen; after it merges, a later record
supersedes that point, as the Status line says.

## Consequences

- **DuckDB's one-process limit stops mattering.** One process owns the
  files, so there is never a reader beside the writer.
- **The pit wall's API can run on any node.** Only `logbook` has to sit on
  the node that holds the volume.
- **`logbook` needs a glibc base image** if DuckDB is chosen (point 6), and
  its build needs cgo. Every other Go service keeps a static binary.
- **`logbook` is on the read path.** When it is down, history and traces are
  unavailable to the pit wall, though the live race carries on (the dash reads
  NATS, ADR 0001 point 6). That is the price of having one owner; v1's pit
  wall could read with the engineer stopped.
- **Every read needs a service in `contracts`.** A new query is a contract
  change first, released before the pit wall or an experiment can use it.
- **`logbook` must be back within `TELEMETRY`'s `max_age`** to resume a lap;
  a longer outage loses the frames of the laps it missed, though not their
  events (`EVENTS` keeps them for 30 d).

Changing any point here needs a new record that supersedes this one, or
supersedes that point alone. A record that settles point 6 or 7 supersedes
only that point: this record's status line and its row in
[`README.md`](README.md) then say "Superseded in part by NNNN (point N)".

## Alternatives considered

- **v1's model: one writer, many readers on the volume.** Keeps the hidden
  contract and the node pinning, and rules out the index in DuckDB.
- **Postgres with TimescaleDB, or ClickHouse.** Servers to run and look
  after, which is more than one driver's laps need.

## References

- v1 ADR 0001, data volume and network:
  https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0001-data-and-network.md
- those-who-run/those-who-run#148 (the file before the row),
  those-who-run/those-who-run#183 (index reads),
  those-who-run/those-who-run#189 (the job runner)
- DuckDB concurrency: https://duckdb.org/docs/current/connect/concurrency.html
- DuckDB Go driver: https://github.com/duckdb/duckdb-go
- NATS JetStream consumers: https://docs.nats.io/nats-concepts/jetstream/consumers
- NATS services (micro): https://docs.nats.io/using-nats/developer/services
