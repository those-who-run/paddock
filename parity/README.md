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
image (0.1.10, with `ecu replay`) and its own entrypoint, so the service's
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
in order onto one store and one bus as one engineer stopped and started again
(#76), so a session can continue across them: v1 shut down (its results card
held) and started again on the same store; `ecu replay` under the same source,
each recording but the last with `--keep-open` and each after the first with
`--clock` 8.7 s after the last frame of the one before, read from that
recording (ecu#56's commands). v1's store clock takes the same 8.7 s. A run has
its own stack, so its `ecu` bucket holds no other run's kept session. `--all` is every recording in
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
  place and the one before it), `race.finished`, `session.ended` (its summary,
  below) and `callout` (rule, kind, priority, pit lane and slots). Text, evidence and v1's
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
- `session.ended` compares the session's summary (#86 AC1): `started_at` and
  `ended_at` (epoch milliseconds), `driving_ms`, `paused_ms`, `ended_paused`,
  `car_code`, `car_name`, `total_laps`, `laps_completed`, `best_lap`,
  `best_lap_ms`, `fastest_lap_ms`, `game_best_lap_ms` (v1's `gt7_best_lap_ms`),
  `total_time_ms`, `kind` (the summary's session type, v1's `session_kind`),
  `finished`, `finishing_position`, `final_position`, `total_cars`,
  `joined_on_lap`, `stints`, `pit_stops` (how many), `fuel_burned`,
  `fuel_burned_by_stint`, `fuel_stints` and `fuel_used`, all exactly. v1's side is the
  `summary.json` v1 wrote, as `ecu`'s `internal/race/testdata/summary/make_golden.py`
  reads it, with v1's store on the recording's clock (its first frame at
  2026-01-01T00:00:00Z, as `ecu replay` dates it with no `--clock`). v1 stored
  neither `best_lap` nor `finishing_position`, so they are derived as
  `SessionSummary` defines them: the first lap with v1's best time (complete
  or not when the best is GT7's own, else the first complete one), and
  `final_position` for a lap race that finished. `gaps` are compared, each
  gap's start and end in epoch milliseconds, its lap and its reason (v1's
  "engineer restart" is `engineer_restart`). What is left out (the laps, the
  track and the raw fuel levels) is named in `notCompared`.
- A session the engineer's shutdown ended has no `session.ended` on v1's side,
  as `ecu` publishes none at a stop: v1 holds its results card. If v1 lets go
  of the card instead of continuing the session (another session started, or
  the window passed), its `session.ended` is emitted then, at frame 0, as
  `ecu`'s end from the record has `last_seq` 0.
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
of 1 outside a field (#276), in an event and in the summary, and `ecu`#52's
other two departures from v1's summary: a best lap where v1 took lap 1 as the only
timed lap (#83) and a held start's `started_at`, up to 5 s earlier (#74); and
the maintainer's decision of 2026-10-31, no track at the start (`ecu`#60 and
`radio`#31, from `ecu` 0.1.12): no start brief that only named the track, no
track slot in a race's start brief, and a track in the race summary when `ecu`'s
verdict is CONFIRMED, each on the recordings it lists; and a grid start where v1
read rolling, on `real-nuc-sugo-two-stops-2026-10-18`'s `session.kind` at frame
47 (#148, `ecu`#69, from `ecu` 0.1.14); and the ranked kind (#146, `ecu`#70 and
`ecu`#72, from `ecu` 0.1.15), on the runs with a session v1 read as a timed field
and `ecu` reads as ranked (the 2026-10-14 qualifying, Spa and Suzuka of
2026-10-18, and the restart pair): the kind and the leap's own `session.kind`,
the start brief (`template_ranked`), the rank calls at the driver's own line
crossings, and the quit summary v1 never raised, `ecu`'s own items each pinned to
the frames `rankedRuns` names and a rank call's kind to its direction. An entry for something still to do is removed by the PR that does it. The
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

CI's `parity-events` job runs `make parity-events` from paddock's root:
`--all`, every recording in `recordings/` and then the restart pair, which is
one continued session on both sides (#76): one `session.started`, a
`session.continued` with the carried `session.track` at the second
recording's first frame, and one `session.ended` whose `gaps` hold the
restart.

## What is spoken

```sh
go run . spoken                                  # paddock#64's nine recordings
go run . spoken real-nuc-sugo-two-stops-2026-10-18
go run . spoken --all                            # every recording
make spoken RECORDINGS=real-nuc-fuji-timed-2026-10-15
```

```
parity spoken [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--all] RECORDING…
```

The Brundle demo's scripted check (paddock#64 R1, [the demo](../docs/demos/brundle.md)):
what radio says against what v1 says, callout for callout. Both sides speak with
v1's bundled script off (`script: false`, the one rules file
[`spoken.go`](spoken.go) writes and both mount), since v1 seeds nothing and its
script picks at random: with it off, each callout has one phrase. With no
`RECORDING`, the nine paddock#64's acceptance criteria name: a clean race, two
pit stops, two stops with the lane across the line, a time trial, a timed race,
a session joined part way, and a replay and a demo that must stay silent. The
flags and exit codes are `laps`' own; `--keep` keeps both sides' lines.

- **v1** runs its own `those-who-run replay RECORDING` in v1's image with
  `GT7_RULES_FILE` naming that file, and reads its `ConsolePlayer`'s lines,
  `[t s] (rule) text`, from among its log lines. Its replay drains the queue
  after every frame, so the lines are what v1's queue said, pit-lane holds and
  drops included.
- **v2** brings up a fresh compose project, as for `laps`, then `radio` with a
  compose file laid over the stack's: source `parity`, `console` sink, `instant`
  timing (every callout said in the queue's order, none stale) and
  `TWR_RADIO_VOICE_FILE` naming the same file. Once radio logs
  `callouts: taking`, `ecu replay --src parity --speed max` runs, and radio's
  console lines (`[hh:mm:ss.ss] (rule) text  kind=… id=…`) are read from its log
  once none has come for 5 s. A radio that logs its stop during the run, or is
  not running at the end of it (killed without a word), fails the run.

**The comparison** aligns the two lists on rule and text, in order (their
longest common subsequence), so one missing callout is one difference, not a
cascade. Lines left over pair up by rule within a stretch, as a callout said
otherwise; the rest are said on one side only. A line radio says twice is a
difference. Each difference is printed with the frame its callout was created
on (from 1 in its recording), its time from the first frame, the rule, and both
sides' place and text. Times aren't compared: v1's are the recording's own,
radio's ecu replay's clock, and both are shown from the first frame.

**Known differences** (R3) are in [`known.go`](known.go), each with its recording,
rule, both texts, the decision and the issue that took it: the six of the
decision of 2026-10-31, no track at the start (two start briefs and four race
summaries). A difference no
entry matches fails the run, and so does an entry for a recording that was run
and matched nothing. A real difference in what is said is a bug in radio or
`ecu`, fixed there, not listed.

CI's `parity-spoken` job runs `make brundle-demo` from paddock's root: the nine.
It is in [`parity-laps.yml`](../.github/workflows/parity-laps.yml) beside the
other two runs and, like them, not a required check.

## The pit wall

```sh
go run . pitwall                                 # #133's three recordings, onto one store
make pitwall
```

```
parity pitwall [--v1-image IMG] [--compose FILE] [--healthz URL] [--pitwall URL] [--keep] [RECORDING…]
```

v1's pit wall data API against v2's (paddock#133): the same questions asked of
both over the same recordings. With no `RECORDING`, #133's three, as the Rosberg
plan trims it: a race at Circuit Gilles Villeneuve
(`real-nuc-gilles-villeneuve-race-2026-10-17`), the pit stop at the same track
(`real-nuc-gilles-villeneuve-pit-stop-2026-10-17`: #122 AC2's lap flags, and
#123's traces either side of the cut-scene), whose sessions are each other's
same-track, and Suzuka joined part way (`real-nuc-suzuka-joined-mid-race-2026-10-18`,
#122 AC3's `joined_on_lap`). The recordings go onto one store on each side, in
order, the n-th on a clock n-1 days after 2026-01-01T00:00:00Z, so the list,
its options and same-track have more than one session to work on. `--pitwall`
is the pit wall as compose publishes it (`http://127.0.0.1:9852`); `--keep`
keeps v2's stack and both sides' answers. The exit codes are `laps`' own.

- **v1** runs [`v1pitwall.py`](v1pitwall.py) in v1's image: v1's own `replay`
  of each recording with its `--clock` into one temporary store, then v1's pit
  wall app (`make_app`, what `those-who-run pitwall` serves) asked over HTTP in
  the same process. Nothing in v1 is changed.
- **v2** brings up a fresh compose project, `parity-pitwall`, of compose.yaml's
  own logbook and pit wall, so the run checks the images compose pins and
  Renovate bumps. A compose file of the harness's own laid over the stack's
  (`wallOverride` in [`wallsides.go`](wallsides.go)) sets only the pit wall's
  source, `TWR_PITWALL_SRC=parity`. The pit wall is in compose's `replay`
  profile with radio and podium, so it is started by name after `up -d`, and
  nothing else in the profile starts. It waits for the pit wall's `/healthz` to say `logbook`, runs `ecu
  replay --src parity --speed max --clock T` for each recording, waits for
  logbook's laps (v1's count) and for the list to show every session closed, then
  asks `PitwallService` over Connect's JSON.

**The questions** (R1), on each side: the session list with no filter, then for
each session in it `GetSession`, `ListLaps`, `ListSameTrack`, and `GetTrace` at
1500 points for each complete lap (R4: every complete lap of the three). Every
lap of these recordings has fewer than 1500 frames, so no trace is thinned here:
logbook's `TestTracesEqualV1s` and `TestThinningKeepsThePeaks` check thinning
against v1's own `lap_trace`.

**The comparison.** v2's answers are put in v1's names and shapes first
(`v1Shape`), by contracts' own map of v1's schema (`pitwall_v1_test.go`): every
field, null where it is absent; a session and a trace unwrapped from their
responses; Timestamps as Unix seconds (the maintainer's decision, 2026-10-31);
`best_lap` as `best_lap_no`, `fuel_burned` as `fuel_used`, `track.track_id` as
`track.id`; an absent `total_laps` as v1's 0; and the two fields v1 had no value
for (`card_posted_at`, `from_v1_rows`) left out. v1's lap `file` is left out
(contracts left it out), and so are the ids each side mints.

- **Sessions pair** by their recording (the day they started on) and their
  place in it, in start order (R2). Their start and track are then compared as
  fields, so a session that starts or sits elsewhere is a difference, not a
  missing pair.
- **Values compare** field by field, nested ones by path (`track.name`), lists
  value for value; `started_at` and `ended_at` within 1 ms, fuel within
  0.001 L, everything else exactly (R3). v1 keeps a time as a float of Unix
  seconds and ecu as whole milliseconds, so a millisecond's rounding is all the
  conversion makes; a start that moves further, such as a held start (#74), is a
  difference. A trace compares value for value in
  every channel; a difference says how many values differ and the first. The
  list compares its order, `total`, `matched` and each filter's options with
  their counts; same-track compares which sessions, by pair.
- **The pit lap's jump** (#123 AC2): on each lap v1 flags `pit`, v2's trace must
  have two points more than 20 m apart.

**Accepted differences** (R5) are the `wallAccepted` entries in
[`accepted.go`](accepted.go), each with its reason and the decision or issue
behind it. Each is pinned, with no wildcard: a recording, a session's place in
it (from 1), the routes it may match, the lap where those routes are per lap
(`laps`, `trace`) and none where they aren't, the field, and both values as the
output prints them. A field of the list's own (its order, `total`, `matched`,
options) has no recording, session or lap, and only the `sessions` route. An
entry pinned otherwise is refused before anything runs (exit 2). Any difference
no entry allows fails the run, and so does an entry that allowed none. On the three recordings there are none: the maintainer's
decided departures either don't arise there (the track label for a circuit known
only by its family, a session with no full lap, the ranked kind) or are the
mapping above (Timestamps, contracts' renames, an absent `total_laps`), and the
card's words, from contracts' `format`, are v1's for these sessions. Every
session's start and end are v1's to the millisecond, the race's, 19.5 s into its
recording, among them.

**The output** is a line per difference (recording, session, route, lap, field,
both values and any accepted entry), then a line per recording with the
sessions, laps and traces on each side and the differences, failing and
accepted.

CI's `parity-pitwall` job runs `make parity-pitwall` from paddock's root.

## Checks

`make check` runs the Go checks in the order of `those-who-run/.github`'s
`go.yml`: gofmt, `go mod tidy`, vet, golangci-lint, the tests (race detector,
no skips, coverage floor 90%), build and govulncheck. CI runs the same through
`go.yml` in its `parity / check` job. The tools are pinned in
`tools/go.mod`. The tests need no containers: the comparison runs on canned
laps (AC2, AC3, AC4) and on one real run's items in `testdata/run/` (the pit
stop through v1 1.0.0 and `ecu` 0.1.10's race machine and rules), the callouts on `contracts`' examples
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
