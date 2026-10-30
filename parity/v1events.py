"""v1's side of `parity events` (paddock#104): what v1's engine decided over one
run of recordings, as the harness's normalised JSON lines (README.md, "Events and
callouts").

The harness runs it in v1's own image, whose Python has v1 installed:

    python -c "$(cat v1events.py)" RECORDING [RECORDING…]

It replays each recording as v1's `replay` does, with every rule on and the
bundled script off (`script: false`, so every callout has one template and its
text is the same on every run), into one temporary store, with a new
engine for each recording: a second recording is v1 started again on the same
store, so the restart pair can continue its session. Nothing in v1 is changed: the
engine's, the store's and the rules' methods are wrapped on their instances
(#103 R4), as ecu's testdata/*/make_golden.py scripts wrap them.

Each line is an item: its type, the recording's place in the run (part, from 1),
the session's place in the run (from 1; 0 with none open), the frame it happened
on (at, counted from 1 in its recording, as ecu's seq is in a replay) and its
fields, in v2's names and values. lap.completed has no at: a lap is matched by its
number. A start v1 held and then ignored (a replay or a demo) is session.discarded,
in no session, as of the frame v1 decided on.

Beside the engine's own decisions, v1 had none of these as events, so they are
read where v1 made them (as ecu's testdata/*/make_golden.py scripts read them):
race.final_lap from the final_lap rule's call, race.position from a second
PositionRule with only its 3 s settle (start_grace_secs and min_interval_secs 0,
as ecu's race.position is the fact without the callout's limits), and
session.paused and session.resumed from the engine's PauseClock. Each callout
carries its slots, the values v1 put into the phrases its rule picked.
"""

from __future__ import annotations

import copy
import json
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path

# v1's lap roles, as LapCompleted.pit_role names them.
PIT_ROLES = {None: "none", "in": "in_lap", "out": "out_lap", "in_out": "in_and_out"}
# v1's fuel use, as SessionKind.fuel_use names it.
FUEL_USE = {None: "unknown", True: "on", False: "off"}
# GT7's tyre order, as v1's temps rule reads tire_temp, in contracts' Corner names.
CORNERS = ("CORNER_FRONT_LEFT", "CORNER_FRONT_RIGHT", "CORNER_REAR_LEFT", "CORNER_REAR_RIGHT")
# lap_time's kinds with no delta_ms (contracts v0.2.3's Callout comment).
NO_DELTA = ("delta_first", "template_in_lap", "template_out_lap", "template_pit_lap")
# The wall-clock time of each recording's first frame, as `ecu replay` dates it
# with no --clock, and as ecu's testdata/summary/make_golden.py runs v1's store:
# the store's clock moves with the frames' own times (v1's RecordingClock).
EPOCH = datetime(2026, 1, 1, tzinfo=timezone.utc).timestamp()


def callout_kind(rule: str, keys: list[str], briefed: bool) -> str:
    """The kind contracts v0.2.3 names for a callout (Callout's comment, "The kinds
    ecu sends, by rule"), from the phrase keys its rule picked for it, in order. A
    situational pool counts as the rule chose it, configured or not: ecu owns the
    settings that choose it. briefed: start_brief had briefed before this frame.

    - start_brief: template for the brief, template_joined for a joined brief,
      template_track for the track named after the brief.
    - race_summary: the key that fills {position}; else template_quit for leaving
      a solo session; else template (no field, no position said).
    - every other rule: the first key, which is the one its sentence turns on
      (lap_time's {delta} before its template, a new best's own template).
    """
    if rule == "start_brief":
        if "template_joined" in keys:
            return "template_joined"
        return "template_track" if briefed else "template"
    if rule == "race_summary":
        position = [k for k in keys if k.startswith("template_position")]
        if position:
            return position[0]
        return "template_quit" if "template_quit" in keys else "template"
    return keys[0] if keys else ""


def present(slots: dict) -> dict:
    """The slots as protojson gives ecu's: a slot not set, an empty track, a false
    timed and no corners are left out. A 0 is kept, as an optional int32 is."""
    return {k: v for k, v in slots.items() if v is not None and v != "" and v is not False and v != []}


def brief_slots(keys: list[str], tracks: list[str], state) -> dict:
    """start_brief's slots, from the phrase keys it picked, in contracts v0.2.3's
    names, as ecu's internal/rules/startbrief/testdata/make_golden.py gives them:
    the name it spoke for {track}, the frame's total_laps for {laps}, laps_left
    for template_laps_to_go, the frame's position and total_cars for {p} and
    {total}, and current_lap for template_joined's {lap}, with total_laps beside
    it when the race has a lap total."""
    out: dict = {}
    for key in keys:
        if key == "template_track":
            out["track"] = tracks.pop(0) if tracks else None
        elif key == "template_laps":
            out["laps"] = state.total_laps
        elif key == "template_laps_to_go":
            out["laps_left"] = state.laps_left
        elif key == "template_timed":
            out["timed"] = True
        elif key in ("template_position", "template_joined_position"):
            out["position"], out["total_cars"] = state.position, state.total_cars
        elif key == "template_joined":
            out["lap"] = state.current_lap
            if state.has_lap_count:
                out["laps"] = state.total_laps
    return out


def secs_ms(secs):
    return None if secs is None else round(secs * 1000)


def summary_fields(summary: dict) -> dict:
    """session.ended's fields from the summary.json v1 wrote, in contracts v0.2.3's
    SessionSummary names, as ecu's internal/race/testdata/summary/make_golden.py
    reads it (paddock#86 AC1). The laps are `parity laps`'s. Times are epoch
    milliseconds. Two v1 never stored are derived where v1 decided them, as
    SessionSummary defines them: best_lap is the first lap with v1's best_lap_ms,
    whatever its completeness when that best is GT7's own, else the first
    complete one; finishing_position is final_position for a lap race that
    finished."""
    best_ms = summary["best_lap_ms"]
    gt7 = best_ms is not None and best_ms == summary["gt7_best_lap_ms"]
    best = next(
        (
            lap["lap"]
            for lap in summary["laps"]
            if (gt7 or lap["complete"]) and best_ms and lap["lap_time_ms"] == best_ms
        ),
        None,
    )
    kind = (summary["session_kind"] or {}).get("kind")
    finishing = summary["final_position"] if summary["finished"] and kind == "lap_race" else None
    return {
        "started_at": secs_ms(summary["started_at"]),
        "ended_at": secs_ms(summary["ended_at"]),
        "driving_ms": summary["driving_ms"],
        "paused_ms": summary["paused_ms"],
        "ended_paused": summary["ended_paused"],
        "car_code": summary["car_code"],
        "car_name": summary["car_name"],
        "total_laps": summary["total_laps"],
        "laps_completed": summary["laps_completed"],
        "best_lap": best,
        "best_lap_ms": best_ms,
        "fastest_lap_ms": summary["fastest_lap_ms"],
        "game_best_lap_ms": summary["gt7_best_lap_ms"],
        "total_time_ms": summary["total_time_ms"],
        "kind": kind,
        "finished": summary["finished"],
        "finishing_position": finishing,
        "final_position": summary["final_position"],
        "total_cars": summary["total_cars"],
        "joined_on_lap": summary["joined_on_lap"],
        "stints": summary["stints"],
        "pit_stops": len(summary["pit_stops"]),
        "fuel_burned": summary["fuel_burned"],
        "fuel_burned_by_stint": summary["fuel_burned_by_stint"],
        "fuel_stints": summary["fuel_stints"],
        "fuel_used": summary["fuel_used"],
    }


def emit(item, part, session, at, **fields):
    print(json.dumps({"type": item, "part": part, "session": session, "at": at, "fields": fields}))


class Clock:
    """v1's store clock on a recording's frames: the first at EPOCH, each later one
    EPOCH plus its time since the first (v1's RecordingClock)."""

    def __init__(self) -> None:
        self.now = EPOCH

    def __call__(self) -> float:
        return self.now


class Run:
    """The recordings of one run, in order, through v1 onto one store."""

    def __init__(self, store_dir: Path) -> None:
        self.store_dir = store_dir
        self.sessions: dict[str, int] = {}  # v1's session id: its place in the run
        self.part = 0
        self.frame = 0
        self.frame_of: dict[tuple[int, float], list[int]] = {}
        self.seen: dict[int, int] = {}  # id(state) of a stepped frame: its frame
        self.entry_at: int | None = None  # the frame of the open pit entry
        self.teleports = 0
        self.raised: list = []  # each callout: it, part, session, frame, kind, pit lane
        self.outcome: dict[int, str] = {}  # id(callout): held or dropped in the pit lane
        self.at_entry = False  # in _apply_pits: the queue sorted at a pit entry
        self.line = 0  # the recording's line engine.process is on
        self.held_at: int | None = None  # the line of the start v1 holds
        self.pause: dict = {}  # the pause session.paused was written for
        self.pauses = 0  # the session's pauses so far, for the pause's number
        self.frame_end: list = []  # what a rule's slots clear after each frame
        self.restore: list = []  # what puts back a v1 module's function wrapped for a replay
        self.clock = Clock()  # the store's clock, on the recording's frames

    def session(self, store: SessionStore) -> int:
        s = store.session
        return 0 if s is None else self.sessions.get(s.session_id, 0)

    def frame_at(self, state) -> int:
        return self.seen.get(id(state), self.frame)

    def replay(self, path: Path, last: bool) -> None:
        # Here, not at the top: the module's pure parts are tested without v1.
        from those_who_run.callouts import CalloutQueue
        from those_who_run.config import DEFAULT_RULES, validate_templates
        from those_who_run.engine import Engine
        from those_who_run.replay import read_recording
        from those_who_run.rules import build_rules
        from those_who_run.rules.position import PositionRule
        from those_who_run.store import SessionStore

        self.part += 1
        part = self.part
        states = list(read_recording(path, strict=True))
        self.frame_of = {}
        for n, s in enumerate(states, 1):
            self.frame_of.setdefault((s.packet_id, s.t), []).append(n)
        self.seen = {}
        self.line, self.held_at = 0, None
        self.clock.now = EPOCH
        store = SessionStore(self.store_dir, clock=self.clock)
        cfg = copy.deepcopy(DEFAULT_RULES)
        validate_templates(cfg)
        rules = build_rules(cfg["rules"])
        # race.position's own PositionRule: in the engine, so it sees every frame and
        # every pit, track and session the others see, but it raises nothing.
        settled = PositionRule({"start_grace_secs": 0, "min_interval_secs": 0})
        engine = Engine([*rules, settled], CalloutQueue(max_age=None), store)
        self.wrap_engine(engine, store, part)
        self.wrap_store(engine, store, part)
        for rule in rules:
            self.wrap_rule(rule, store, part)
        self.wrap_decisions(rules, settled, store, part)
        t0 = states[0].t if states else 0.0
        try:
            for state in states:
                self.clock.now = EPOCH + (state.t - t0)
                engine.process(state)
                while engine.queue.pop(state.t) is not None:
                    pass
            # A recording before the last ends as the engineer stopping: the next
            # one may continue its session (#273).
            engine.close_session(shutdown=not last)
        finally:
            store.close()
            for undo in self.restore:
                undo()
            self.restore, self.frame_end = [], []

    def wrap_engine(self, engine: Engine, store: SessionStore, part: int) -> None:
        step = engine._step
        pending: dict[str, object] = {}

        def finish_check() -> None:
            """race.finished once Engine.finished goes true in a step that began with
            the session open, as testdata/finish/make_golden.py writes it."""
            if not pending or pending["finished"] or not engine.finished or not pending["open"]:
                return
            state = pending["state"]
            pending.clear()
            if state.phase.name == "FINISHED":
                kind, at = "flag", state
            else:
                kind, at = "timed", engine._crossing
            position = at.position
            emit(
                "race.finished",
                part,
                self.session(store),
                self.frame_at(at),
                kind=kind,
                lap=at.current_lap - 1,
                position=position,
            )

        def _step(state):
            self.frame = self.frame_of[(state.packet_id, state.t)].pop(0)
            self.seen[id(state)] = self.frame
            pending.update(finished=engine.finished, open=store.session is not None, state=state)
            out = step(state)
            finish_check()
            pending.clear()
            return out

        engine._step = _step
        engine.finish_check = finish_check

        apply_kind = engine._apply_kind

        def _apply_kind(kind):
            if store.session is not None:
                k = kind.record()
                emit(
                    "session.kind",
                    part,
                    self.session(store),
                    self.frame,
                    kind=k["kind"],
                    total_laps=k["total_laps"] or None,  # 0 is no lap total, as v2 leaves it out
                    field=k["field"],
                    fuel_use=FUEL_USE[k["fuel_use"]],
                    cars_on_track=k["cars_on_track"],
                    start=k["start"],
                )
            apply_kind(kind)

        engine._apply_kind = _apply_kind

        apply_track = engine._apply_track

        def _apply_track(match, **kw):
            before = engine.track
            out = apply_track(match, **kw)
            if engine.track is not before and kw.get("store", True) and store.session is not None:
                emit(
                    "session.track",
                    part,
                    self.session(store),
                    self.frame,
                    track_id=match.track_id,
                    confidence=match.confidence,
                    family=match.family or "",
                )
            return out

        engine._apply_track = _apply_track

        apply_pits = engine._apply_pits

        def _apply_pits(status, state):
            session = self.session(store)
            if len(status.teleports) > self.teleports:
                jump = status.teleports[-1]
                emit("pit.teleport", part, session, self.frame, lap=jump.lap, entry_at=self.entry_at)
                self.entry_at = None
            elif status.in_pit_lane:
                stop = status.stops[-1].record()
                self.entry_at = self.frame
                emit(
                    "pit.entered",
                    part,
                    session,
                    self.frame,
                    stop=stop["stop"],
                    in_lap=stop["in_lap"],
                    fuel_before=stop["fuel_before"],
                    position_before=stop["position_before"],
                )
            else:
                stop = status.stops[-1].record()
                self.entry_at = None
                emit(
                    "pit.exited",
                    part,
                    session,
                    self.frame,
                    stop=stop["stop"],
                    stint=status.stint,
                    in_lap=stop["in_lap"],
                    out_lap=stop["out_lap"],
                    pit_lane_ms=secs_ms(stop["pit_lane_secs"]),
                    stationary_ms=secs_ms(stop["stationary_secs"]),
                    fuel_before=stop["fuel_before"],
                    fuel_after=stop["fuel_after"],
                    fuel_added=stop["fuel_added"],
                    tyres_changed=stop["tyres_changed"],
                    position_before=stop["position_before"],
                    position_after=stop["position_after"],
                )
            self.teleports = len(status.teleports)
            self.at_entry = True  # what _quiet sorts now was queued before the entry
            try:
                return apply_pits(status, state)
            finally:
                self.at_entry = False

        engine._apply_pits = _apply_pits

        reset = engine.pits.reset

        def pits_reset():
            self.teleports, self.entry_at = 0, None
            return reset()

        engine.pits.reset = pits_reset

        quiet = engine._quiet

        def _quiet(callouts):
            out = quiet(callouts)
            spoken = {id(c) for c in out}
            for c in callouts:
                if id(c) not in spoken:
                    outcome = {"hold": "held", "drop": "dropped"}[engine._policy(c)]
                    # Raised in the lane and dropped: ecu applies the pit lane to
                    # what it raises there and never publishes such a callout
                    # (contracts' PitLane). One queued at the entry is radio's.
                    if outcome == "dropped" and not self.at_entry:
                        outcome = "unpublished"
                    self.outcome[id(c)] = outcome
            return out

        engine._quiet = _quiet

        process = engine.process

        def _process(state):
            """v1's start check: a start held for GT7's cars-on-track flag, then
            ignored as a replay or a demo, is session.discarded, as of the frame v1
            decided, with the frame the start was on, as ecu's (#74). Then what a
            rule's slots kept for the frame is cleared."""
            self.line += 1
            held, ignoring = bool(engine._held), engine.replay
            out = process(state)
            if not held and engine._held:
                self.held_at = self.line
            if not ignoring and engine.replay:
                emit(
                    "session.discarded",
                    part,
                    0,
                    self.line,
                    reason=engine.ignoring,
                    first_seq=self.held_at,
                    evidence=engine.driver.why,
                )
            for clear in self.frame_end:
                clear()
            return out

        engine.process = _process

        clock = engine.pauses
        observe = clock.observe

        def observe_pause(state):
            """session.paused when the PauseClock's since goes from none to a time in an
            open session, and session.resumed when it goes back, as ecu's
            internal/race/testdata/pause/make_golden.py reads them. Both on the
            recording's line: a paused frame never reaches _step."""
            since, before = clock.since, clock.paused_secs
            out = observe(state)
            if since is None and clock.since is not None and store.session is not None:
                self.pauses += 1
                kind = "frozen" if state.phase.name == "IN_RACE" else "flagged"
                self.pause = {"session": self.session(store), "kind": kind, "pause": self.pauses}
                emit(
                    "session.paused",
                    part,
                    self.pause["session"],
                    self.line,
                    kind=kind,
                    since_t=clock.since,
                    paused_ms=secs_ms(before),
                    pause=self.pauses,
                )
            elif since is not None and clock.since is None and self.pause:
                emit(
                    "session.resumed",
                    part,
                    self.pause["session"],
                    self.line,
                    kind=self.pause["kind"],
                    since_t=since,
                    pause_ms=secs_ms(state.t - since),
                    paused_ms=secs_ms(clock.paused_secs),
                    pause=self.pause["pause"],
                )
                self.pause = {}
            return out

        clock.observe = observe_pause

    def wrap_decisions(self, rules, settled, store: SessionStore, part: int) -> None:
        """race.final_lap and race.position, which v1 decided inside its rules (ecu's
        internal/race/testdata/final-lap-position/make_golden.py): the final lap
        when the final_lap rule calls it, and a settled position when the second
        PositionRule would call it, whose callouts go nowhere. Only with a session
        open, as ecu publishes only into one."""
        for rule in rules:
            if rule.name == "final_lap":
                final_evaluate = rule.evaluate

                def _final(prev, state, final_evaluate=final_evaluate):
                    out = final_evaluate(prev, state)
                    if out and store.session is not None:
                        emit("race.final_lap", part, self.session(store), self.frame, lap=state.current_lap)
                    return out

                rule.evaluate = _final
        evaluate = settled.evaluate

        def _settled(prev, state):
            before = settled.announced
            if evaluate(prev, state) and store.session is not None:
                emit(
                    "race.position",
                    part,
                    self.session(store),
                    self.frame,
                    previous_position=before,
                    position=state.position,
                )
            return []

        settled.evaluate = _settled
        settled.on_end = lambda last, state: []
        settled.on_finish = lambda state: []

    def wrap_store(self, engine: Engine, store: SessionStore, part: int) -> None:
        start = store.start_session

        def start_session(state, **kw):
            session = start(state, **kw)
            if session.session_id in self.sessions:
                emit("session.continued", part, self.sessions[session.session_id], self.frame)
            else:
                self.pauses = 0  # a pause's number counts from 1 in each session
                self.sessions[session.session_id] = len(self.sessions) + 1
                emit(
                    "session.started",
                    part,
                    self.sessions[session.session_id],
                    self.frame,
                    car_code=state.car_code,
                    total_laps=state.total_laps or None,
                    joined_on_lap=session.joined_on_lap,
                    # A held start (the start check): the line v1 decided on, which
                    # its clock dated the session from. Shown, never compared.
                    decided_at=self.line if self.frame < self.line else None,
                )
            return session

        store.start_session = start_session

        flush = store._flush_lap

        def _flush_lap(lap_no, state, complete):
            s = store.session
            laps = len(s.laps) if s is not None else 0
            out = flush(lap_no, state, complete)
            if s is not None and len(s.laps) > laps:
                lap = s.laps[-1]
                emit(
                    "lap.completed",
                    part,
                    self.session(store),
                    None,
                    lap=lap["lap"],
                    lap_time_ms=lap["lap_time_ms"],
                    complete=lap["complete"],
                    pit_role=PIT_ROLES[lap["pit"]],
                )
            return out

        store._flush_lap = _flush_lap

        end = store.end_session

        def end_session(state, **kw):
            engine.finish_check()  # a finish in the step that ends the session comes first
            session = self.session(store)
            out = end(state, **kw)
            if session and out is not None:
                summary = json.loads(Path(out).read_text())
                emit("session.ended", part, session, self.frame_at(state), **summary_fields(summary))
            return out

        store.end_session = end_session

    def wrap_rule(self, rule, store: SessionStore, part: int) -> None:
        """Each callout with the phrase keys its rule picked for it, in order, and
        whether start_brief had briefed before the frame: callout_kind makes the
        kind v2 publishes from them (paddock#71). Of a situational pool the rule's
        own first choice counts, configured or not."""
        keys: list[str] = []
        pick, first = rule.phrases.pick, rule.phrases.first
        inside = [False]

        def _pick(key, default):
            if not inside[0]:
                keys.append(key)
            return pick(key, default)

        def _first(options, default):
            if options:
                keys.append(options[0])
            inside[0] = True
            try:
                return first(options, default)
            finally:
                inside[0] = False

        rule.phrases.pick, rule.phrases.first = _pick, _first
        slots_of = self.wrap_slots(rule)
        briefed = [False]
        evaluate = rule.evaluate

        def _evaluate(prev, state):
            briefed[0] = bool(getattr(rule, "briefed", False))
            return evaluate(prev, state)

        rule.evaluate = _evaluate
        make = rule.callout

        def callout(text, state, priority=None):
            c = make(text, state, priority)
            picked = list(keys)
            keys.clear()
            kind = callout_kind(rule.name, picked, briefed[0])
            slots = present(slots_of(kind, picked, state))
            self.raised.append((c, part, self.session(store), self.frame, kind, rule.pit_lane, slots))
            return c

        rule.callout = callout

    def wrap_slots(self, rule):
        """The function that gives a callout's slots (contracts v0.2.3's names) from
        its kind, the phrase keys picked and the frame, with what it needs from the
        rule wrapped on the instance, as each rule's make_golden.py in ecu's
        internal/rules/*/testdata reads it. final_lap's and telemetry's have none."""
        name = rule.name
        if name == "lap_time":
            from those_who_run.state import posted_lap

            best = [0]  # the best the rule measures against, as LapTimeRule.evaluate takes it
            evaluate = rule.evaluate

            def _evaluate(prev, state):
                best[0] = 0
                if prev is not None:
                    best[0] = prev.best_lap_ms if prev.best_lap_ms > 0 else rule.own_best_ms
                return evaluate(prev, state)

            rule.evaluate = _evaluate

            def lap_time(kind, _keys, state):
                out = {"lap": posted_lap(state), "lap_time_ms": state.last_lap_ms}
                if kind not in NO_DELTA:
                    out["delta_ms"] = state.last_lap_ms - best[0]
                return out

            return lap_time
        if name == "fuel":
            import those_who_run.rules.fuel as fuel_module

            said: list[int] = []
            laps_words = fuel_module.laps_words

            def _laps_words(n):
                said.append(n)
                return laps_words(n)

            fuel_module.laps_words = _laps_words  # the module's: put back after the replay
            self.restore.append(lambda: setattr(fuel_module, "laps_words", laps_words))

            def fuel(kind, _keys, _state):
                laps = said[-1] if said and kind in ("template", "template_short") else None
                said.clear()
                return {"laps": laps}

            return fuel
        if name == "position":
            before = [None]
            evaluate = rule.evaluate

            def _evaluate(prev, state):
                before[0] = rule.announced
                return evaluate(prev, state)

            rule.evaluate = _evaluate
            return lambda _kind, _keys, state: {"position": state.position, "previous_position": before[0]}
        if name == "start_brief":
            tracks: list[str] = []
            track_phrase = rule._track_phrase

            def _track_phrase(track):
                tracks.append(track)
                return track_phrase(track)

            rule._track_phrase = _track_phrase

            def brief(_kind, keys, state):
                out = brief_slots(keys, tracks, state)
                tracks.clear()
                return out

            return brief
        if name == "race_summary":
            said: dict = {}
            self.frame_end.append(said.clear)  # a slot said for no callout is not the next's
            on_finish, on_end, best_words = rule.on_finish, rule.on_end, rule._best_words

            def _on_finish(state):
                if not rule.announced and rule.has_field(state) and state.position is not None:
                    said["position"] = state.position
                return on_finish(state)

            def _on_end(last, state):
                said["laps"] = max(rule.laps_done, last.current_lap - 1)
                return on_end(last, state)

            def _best_words(state):
                best = state.best_lap_ms if state.best_lap_ms > 0 else rule.best_ms or rule.best_any_ms
                if best > 0:
                    said["best_lap_ms"] = best
                return best_words(state)

            rule.on_finish, rule.on_end, rule._best_words = _on_finish, _on_end, _best_words

            def summary(_kind, _keys, _state):
                out = dict(said)
                said.clear()
                return out

            return summary
        if name == "temps":
            before: list = []
            evaluate = rule.evaluate

            def _evaluate(prev, state):
                # reset() makes new alarms on every start, so they are read on the call.
                before[:] = [a.fired_at for a in getattr(rule, "tyres", [])]
                return evaluate(prev, state)

            rule.evaluate = _evaluate

            def temps(kind, _keys, _state):
                # The tyres whose alarm fired on this frame: the one that fired and
                # those that joined it, which the rule has set before it says them.
                if kind != "template_tyre_hot":
                    return {}
                return {"corners": [CORNERS[i] for i, a in enumerate(rule.tyres) if a.fired_at != before[i]]}

            return temps
        return lambda _kind, _keys, _state: {}

    def write_callouts(self) -> None:
        """Each callout raised, after the run, when v1's pit lane has had its say."""
        for c, part, session, at, kind, pit_lane, slots in self.raised:
            outcome = self.outcome.get(id(c), "spoken")
            if outcome == "unpublished":
                continue
            emit(
                "callout",
                part,
                session,
                at,
                rule=c.rule,
                kind=kind,
                priority=c.priority,
                pit_lane=pit_lane,
                slots=slots,
                text=c.text,
                outcome=outcome,
            )


def main(paths: list[Path]) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        run = Run(Path(tmp))
        for n, path in enumerate(paths, 1):
            run.replay(path, last=n == len(paths))
    run.write_callouts()


if __name__ == "__main__":
    main([Path(p) for p in sys.argv[1:]])
