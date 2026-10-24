# 0007: The store's engine, DuckDB in one file

**Status:** Accepted, 2026-10-24. Asked for by #37. Supersedes
[ADR 0003](0003-the-store.md), points 6 and 7.

## Context

[ADR 0003](0003-the-store.md) left two choices open: the engine `logbook`
stores laps with (point 6, DuckDB as the lean, SQLite plus Parquet in pure Go
as the fallback), and whether the index lives in the same engine (point 7).
Its analysis and its table of the two engines stand as the record of why.

#37, which writes each lap's Parquet file and its index rows, needs both
answered. The maintainer settled them in the triage of #37 on 2026-10-24
([the decision](https://github.com/those-who-run/paddock/issues/37#issuecomment-6385121891)).
An accepted record is not edited, so this record carries those choices.

## Decision

### 1. DuckDB, embedded in `logbook`

`logbook` embeds DuckDB through the official Go driver,
[`github.com/duckdb/duckdb-go/v2`](https://github.com/duckdb/duckdb-go).
DuckDB writes the Parquet lap files itself (`COPY … TO`).

The cost is cgo and a glibc base image (`distroless/cc`), about 30 MB more.
It stays inside `logbook`'s image: `chassis` and every other Go repository
stay cgo-free with a static binary.

Why: the features after parity (debriefs, personal bests, corner coaching,
reference laps) are analytics over laps, which is what DuckDB is for, and v1
already relies on DuckDB for its traces.

Settles: ADR 0003 point 6; #9 R1.6 and its second open question; #37's
engine.

### 2. One engine: the index in `index.duckdb`

The index (sessions and laps) lives in DuckDB too, in `index.duckdb`, beside
the Parquet lap files. v1 kept SQLite beside DuckDB because more than one
process read its files. In v2 `logbook` is their only reader and writer
(ADR 0003, points 1 and 2), so that reason is gone. `logbook`'s migrations
cover the index's schema between releases.

Settles: ADR 0003 point 7; #9's third open question; #37's first open
question.

## What #37 does because of this

#37 builds on DuckDB, with the index in `index.duckdb`. This record and its
row in [`README.md`](README.md) mark the decision; ADR 0003's status line and
its index row say "Superseded in part by 0007 (points 6 and 7)", as the
[process](README.md#how) describes. The rest of ADR 0003 still holds.

## Consequences

- **`logbook` needs cgo and a glibc base image.** Its build differs from the
  other Go services', which keep `CGO_ENABLED=0` and a static binary.
- **SQL over the lap files.** Thinning a trace, lap against lap, every session
  at a track and corner statistics are queries, not new Go code.
- **One file format to carry between releases.** The index depends on
  DuckDB's file format, so a DuckDB upgrade that changes it is a `logbook`
  release that migrates its own data.
- **Either choice can be reversed** by a `logbook` release that migrates its
  own data. No contract changes: nobody else sees the files (ADR 0003,
  point 2).

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **SQLite plus Parquet in pure Go**, ADR 0003's fallback: one static binary,
  but each analysis after parity is more Go code or a new tool.
- **DuckDB with a SQLite index beside it**, as v1 does: keeps the index off
  DuckDB's file format, but v1's reason for it, more than one process reading
  the files, does not hold in v2.

## References

- [ADR 0003](0003-the-store.md), points 6 and 7, and their analysis.
- #37, and the maintainer's triage decision on it.
- The DuckDB Go driver: https://github.com/duckdb/duckdb-go
