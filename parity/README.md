# The parity harness

`parity laps` replays a recording through v1 and through v2, normalises the
laps each side stored to the same JSON lines, and compares them (#40).
`parity events` does the same for what each side decided: every event and
callout, v1's from its engine and v2's heard on the bus as `ecu` publishes
them ([below](#events-and-callouts), #104). It is the referee: a v2 piece is
done when the same recording gives the same laps, events and callouts as v1. It is a test tool, not service code, so it is a Go
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

The v2 side runs the compose file's `replay` service (paddock#39): `ecu`'s
image (0.1.9, with `ecu replay`) and its own entrypoint, so the service's
command is `ecu`'s arguments, with the server in `TWR_NATS_URL`.

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

The new end reasons and session types of contracts v0.1.4 are not in the lap
comparison: `logbook export --laps` prints laps only. `parity events` compares
the session's kind; v1 stored no end reason ([accepted.go](accepted.go)).

## Events and callouts

```sh
go run . events                                  # R4's three default recordings
go run . events real-nuc-sugo-two-stops-2026-10-18
go run . events --all                            # every recording, then the restart pair
go run . events a.jsonl.gz+b.jsonl.gz            # two recordings, one after the other
make events RUNS=--all
```

```
parity events [--v1-image IMG] [--compose FILE] [--healthz URL] [--nats URL] [--keep] [--all] RUN…
```

A `RUN` is a recording, as for `laps`, or recordings joined by `+`, replayed
in order onto one store and one bus: v1 started again on the same store, `ecu`
replayed again under the same source. `--all` is every recording in
`recordings/SOURCES.yaml`, then the restart pair
(`real-nuc-suzuka-before-restart-2026-10-18` then
`real-nuc-suzuka-joined-mid-race-2026-10-18`). `--nats` is NATS as the compose
file publishes it (`nats://127.0.0.1:4222`); `--keep` keeps v2's stack and
both sides' items, as for `laps`. The exit codes are `laps`' own.

**v1's side** runs [`v1events.py`](v1events.py) in v1's image, with
`--entrypoint python`: v1's engine over the run's recordings, as v1's `replay`
runs it, with every rule on and the bundled script off (`script: false`), into
a temporary store. Nothing in v1 is changed: the engine's, the store's and the
rules' methods are wrapped on their instances, as `ecu`'s
`testdata/*/make_golden.py` scripts wrap them (#103 R4). A recording before
the last ends as the engineer stopping, so the next can continue its session.

**v2's side** brings up a fresh compose project, as for `laps`, and the
checker subscribes to `twr.v1.parity.evt.>` and `twr.v1.parity.callout.>`
before the first replay (R1). A core subscription hears every event and
callout as `ecu` publishes it, whichever stream keeps it, so `CALLOUTS` being
a work queue doesn't matter, and the same works on a live source (R7). After
each replay it waits until nothing new has come for 2 s. Each message is
decoded into its `contracts` message by its subject; one sent twice (the same
`Nats-Msg-Id`) counts once, as JetStream keeps it once. The stack goes down with
its volumes after the run, so nothing is left behind (AC4).

**The items.** Both sides give one JSON line per event or callout, in v2's
names and values (snake case, enum names without their prefix in lower case,
`null` for a value left out, integers as numbers):

```json
{"type":"pit.entered","part":1,"session":1,"at":455,"fields":{"stop":1,"in_lap":3,"fuel_before":22.899,"position_before":1}}
```

- `part` is the recording's place in the run, from 1, and `session` the
  session's place in the run, from 1 (0 with none open): each side mints its
  own ids.
- `at` is the frame, counted from 1 in its recording: `ecu`'s `seq` in a
  replay, and the recording's line on v1's side. `session.started` takes its
  `first_seq`, `session.ended` its `last_seq`, `race.finished` the frame it is
  as of (the flag, or the timed finish's line). A lap has none: it is matched
  by its number.
- The types and the fields compared are `compared` in [`items.go`](items.go):
  `session.started`, `session.continued`, `session.kind`, `session.track`,
  `session.discarded` (replay or demo, and the held start's first frame),
  `session.paused` and `session.resumed` (flagged or frozen, when it began, the
  pause's length and number, and the time paused so far), `pit.entered`,
  `pit.exited`, `pit.teleport`, `lap.completed` (its time, whether it is
  complete and its pit role), `race.final_lap`, `race.position` (the settled
  place and the one before it), `race.finished`, `session.ended` (finished) and
  `callout` (rule, kind, priority, pit lane and slots). Text, evidence and v1's
  pit-lane outcome (spoken, held or dropped) are shown, not compared.
- v1 had no event for some of these, so `v1events.py` reads them where v1 decided
  them, as `ecu`'s `testdata/*/make_golden.py` scripts do: `race.final_lap` from
  the final lap rule's call, `race.position` from a second `PositionRule` with
  only its 3 s settle (no start grace, no interval, as `ecu`'s event is the fact
  without the callout's limits), and the pauses from the engine's `PauseClock`.
  `lap.best` has no v1 side: v1 kept only the session's best, with a fallback
  the lap model departs from (#83), so `ecu`'s are counted, not compared.
- A callout's kind on v1's side is the kind contracts v0.2.3 names for it
  (`callout_kind` in `v1events.py`, from the phrase keys its rule picked, with
  a situational pool counting as the rule chose it): `start_brief` is
  `template`, `template_track` or `template_joined`; `race_summary` the key of
  its `{position}`, else `template_quit`, else `template`; the others their
  first key (`lap_time`'s `{delta}` key, a new best's own). Its pit lane is the
  rule's `pit_lane` policy, not v1's held or dropped outcome, which waits for
  radio's #59. A callout v1 raised in the pit lane and dropped is left out, as
  ecu never publishes one (contracts' `PitLane`).
- A callout's slots on v1's side are the values its rule put in the phrases it
  picked (`wrap_slots` in `v1events.py`, each rule as its `ecu` golden reads it),
  in contracts v0.2.3's names, with a slot left out where protojson leaves it
  out. A callout with no slots on the bus is the same as one with empty slots.
- A start v1 held for GT7's cars-on-track flag and then ignored as a replay or
  a demo is a `session.discarded` on v1's side, in no session, on the frame v1
  decided on, with the frame the start was on as `first_seq`.

**Matching** (R2): items pair by part, type, a callout's rule or a lap's number,
and frame, within the type's frame tolerance (`frameTolerance`, zero for every
type until one has a written reason). Two callouts of one rule on one frame pair
by kind. v1 may apply two tracks on one frame (the one carried from the last
race, then the frame's own) where ecu publishes the frame's last, so only v1's
last track of a frame is compared; two of ecu's on one frame are a difference.
Numbers compare within a field's tolerance (fuel 0.001 L, pit times
10 ms, v1's rounding), the rest exactly. An item on one side only is a
difference. v2's items of a type v1's side doesn't give yet (`race.position`,
say) are counted and named, not compared.

**Accepted differences** (R4) are in [`accepted.go`](accepted.go), the one
reviewed list, each with its reason and the issue that accepted it: a position
of 1 outside a field (#276), and what `ecu` does not do yet (joined part way,
#75). An entry for something still to do is removed by the PR that does it. The
fields left out on purpose (v1's session type, which `kind` replaces, the end
reason v1 never stored, the callout's text, `lap.best`) are listed there too. Any other difference fails
the run. After the table, each entry that allowed no difference is named; when
the runs cover every recording in `recordings/` (CI's, and `--all`), such an
entry is stale and fails the run until it is removed.

**The output** lists each run's differences, a line each with the part, the
frame (`843/844` where a tolerance let them differ), the session, the item, the
field and both values, and any accepted entry's id; then a table (R5) with a line
per run: events and callouts on each side and matched, the differences, and
those accepted.

CI's `parity-events` job runs `make parity-events` from paddock's root: every
recording in `recordings/`. The restart pair is left out until `ecu` continues
a session after a restart (#76); `--all` runs it.

## Checks

`make check` runs the Go checks in the order of `those-who-run/.github`'s
`go.yml`: gofmt, `go mod tidy`, vet, golangci-lint, the tests (race detector,
no skips, coverage floor 90%), build and govulncheck. CI runs the same through
`go.yml` in its `parity / check` job. The tools are pinned in
`tools/go.mod`. The tests need no containers: the comparison runs on canned
laps (AC2, AC3, AC4) and on one real run's items in `testdata/run/` (the pit
stop through v1 1.0.0 and `ecu` 0.1.9's race machine and rules), the callouts on `contracts`' examples
(`testdata/contracts/`, v0.2.3), and the two sides run against a recorded
command runner, a fake `/healthz` and a fake listener.

CI's `parity-laps` job
([`parity-laps.yml`](../.github/workflows/parity-laps.yml)) runs the harness
itself on the default recordings (R6), as `make parity` from paddock's root:
for PRs and pushes to `main` that touch `deploy/`, `recordings/`, the harness
or the Makefile, every Monday, and by hand. It is not a required check.

Still to come (#40): pinning a lap difference on `ecu` or on `logbook` (R1).
`parity events` hears `ecu`'s `lap.completed` on the bus, but compares only
its time, completeness and pit role.
