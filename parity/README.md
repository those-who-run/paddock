# The parity harness

`parity laps` replays a recording through v1 and through v2, normalises the
laps each side stored to the same JSON lines, and compares them (#40). It is
the walking skeleton's referee: a v2 piece is done when the same recording
gives the same laps as v1. It is a test tool, not service code, so it is a Go
module of its own here, beside the compose stack it drives in
[`deploy/compose/`](../deploy/compose/README.md).

```sh
cd parity
go run . laps                                   # R4's three default recordings
go run . laps real-race-2laps-2026-10-14        # a file in recordings/, by name
go run . laps --keep --frames ~/drives/20261101-120000.jsonl.gz
make laps RECORDINGS=real-nuc-suzuka-pit-stop-2026-10-15
```

```
parity laps [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--frames] RECORDING…
```

- `--v1-image`: v1's image. The default is `muldoon/those-who-run:1.0.0`, by tag
  and by the digest of its index.
- `--compose`: the compose file whose images are v2. The default is
  `deploy/compose/compose.yaml` in the checkout around the working directory.
- `--healthz`: `logbook`'s `/healthz` as the compose file publishes it. The
  default is `http://127.0.0.1:8080/healthz`; it follows the port when #39's
  replay stack publishes another.
- `--keep`: keep v1's store (a volume, `parity-v1-<recording>`), v2's stack
  (compose project `parity-<recording>`, stopped with its volumes), and both
  sides' normalised laps in a temporary directory. The output says where.
- `--frames`: also compare the frames stored per lap. A difference is printed
  as a note and doesn't fail the run.

The exit code is 0 when every recording matches, 1 when any differs, and 2
when the run is refused (a bad flag, a recording whose sha256 is wrong, a
recording v1 stored no laps of) or a side fails. A recording v1 stored nothing
for has nothing to compare against: a wrong `--v1-image` would otherwise match
a v2 that stored nothing either.

## The recordings

With no `RECORDING`, the harness compares R4's three, from
[`recordings/`](../recordings/README.md): a clean race
(`real-nuc-suzuka-no-stop-2026-10-15`), a race with a pit stop
(`real-nuc-suzuka-pit-stop-2026-10-15`) and restarts
(`real-nuc-sugo-restarts-2026-10-18`). A `RECORDING` is a path, or the name of
a file in `recordings/`, with or without `.jsonl.gz`.

Every recording's sha256 is checked before anything runs (AC3). A file whose
name is in [`recordings/SOURCES.yaml`](../recordings/SOURCES.yaml) must have
the sha256 listed there, wherever it is. Any other file needs a
`<file>.sha256` beside it, in `sha256sum`'s format
(`sha256sum x.jsonl.gz > x.jsonl.gz.sha256`). A recording with neither, or
with another sum, is refused, and the run exits 2 with nothing started.

## The two sides

For each recording:

- **v1** replays it with its own `replay` into a fresh volume:
  `those-who-run replay RECORDING --store /data --clock 2026-01-01T00:00:00Z`.
  Then, in the same image (its Python has `sqlite3`), it reads `index.sqlite`'s
  `laps` with their session's kind, ordered by session start and lap. A
  recording's own times are monotonic, not wall clock, so the clock is the
  time `ecu replay` gives a first frame when it is passed none. Both stores
  carry the same dates, though dates are not compared.
- **v2** brings up a fresh compose project with empty volumes (`up -d`: NATS,
  the stream and KV bootstrap, `logbook`) and waits for `logbook`'s `/healthz`.
  It runs `ecu replay --src parity --speed max RECORDING` through the stack's
  `replay` service, with the recording mounted at `/recording/`. Then it waits
  until `logbook`'s `laps_written` has reached v1's lap count and stayed the
  same for 2 s, or, short of it, has stayed the same for 15 s. The quiet period
  matters: v2 may store a lap more than v1, and `logbook` hands back the events
  it hasn't stored when it stops. It stops `logbook`, runs `logbook export --data /data --laps --src parity`
  and takes the stack down with its volumes.

The images are printed first (AC4): v1's from `--v1-image`, v2's from
`compose --profile replay config --images`, since the `replay` service is in
that profile and compose lists only the services of active profiles.

The harness runs `docker` and `docker compose`. `CONTAINER` and `COMPOSE` name
others, as for paddock's own targets
(`CONTAINER=podman COMPOSE='podman compose'`). v2's stack publishes the
compose file's ports on 127.0.0.1, so a `make up` stack must be down first.

The v2 side needs two things that are not in `paddock` yet: an `ecu` release
with `ecu replay` (paddock#35), and the compose file's `replay` service
(paddock#39), whose command is `ecu`'s arguments. Until both land, the v1 side
and the comparison run, and the v2 side fails at `replay`.

## What is compared

One JSON object per lap (R2), with each lap matched by its session's place in
the run and its lap number:

```json
{"session":1,"lap":3,"lap_time_ms":82544,"complete":true,"fuel_start":37.908,"fuel_end":22.899,"fuel_used":15.009,"position":1,"frames":166}
```

- `session` is the session's place in the run (1, 2, …), in start order. Each
  side mints its own session ids, so ids are not compared.
- Fuel (`fuel_start`, `fuel_end`, `fuel_used`) is equal within 0.001 L. A
  difference prints to 4 decimals, so a failing one never looks as if it is
  inside the tolerance.
- `lap_time_ms`, `complete` and `position` are equal or both null.
- `frames` is compared only under `--frames`, as a note.
- **Position in a solo session.** Since contracts v0.1.4, `ecu` leaves position
  out in a session with no field, where GT7 reports 1 and v1 stored it
  (paddock#276). v1's 1 in a `solo_laps` or `solo_timed` session is read as
  null.
- A lap on one side only, or stored more than once on either side, is a
  difference (`lap`, with `stored`, `missing` or `stored 2 times`).
- Every line a side's store gives must carry every one of these keys (and
  `session_id`). A missing key is an error, not a null or a false that could
  match the other side's.

The output is a table per recording: the sessions with laps and the laps on
each side (sessions without laps aren't in either side's lap export), then
each difference with its session, lap, field and both values. After the tables
comes a line saying how many recordings matched.

The new end reasons and session types of contracts v0.1.4 are not compared
yet. `logbook export --laps` prints laps only, with no session columns, and R2
compares laps. Comparing them needs a session export from `logbook` (or the
`EVENTS` read, below).

## Checks

`make check` runs the Go checks in the order of `those-who-run/.github`'s
`go.yml`: gofmt, `go mod tidy`, vet, golangci-lint, the tests (race detector,
no skips, coverage floor 90%), build and govulncheck. CI runs the same through
`go.yml` in its `parity / check` job. The tools are pinned in
`tools/go.mod`. The tests need no containers: the comparison runs on canned
laps (AC2, AC3, AC4), and the two sides run against a recorded command runner
and a fake `/healthz`.

Still to come (#40): the `lap.completed` events read from `EVENTS`, so a
difference can be pinned on `ecu` or on `logbook` (R1), and the CI job that
runs the harness itself on the default recordings (R6).
