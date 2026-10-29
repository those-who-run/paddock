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
number.
"""

import copy
import json
import sys
import tempfile
from pathlib import Path

from those_who_run.callouts import CalloutQueue
from those_who_run.config import DEFAULT_RULES, validate_templates
from those_who_run.engine import Engine
from those_who_run.replay import read_recording
from those_who_run.rules import build_rules
from those_who_run.store import SessionStore

# v1's lap roles, as LapCompleted.pit_role names them.
PIT_ROLES = {None: "none", "in": "in_lap", "out": "out_lap", "in_out": "in_and_out"}
# v1's fuel use, as SessionKind.fuel_use names it.
FUEL_USE = {None: "unknown", True: "on", False: "off"}


def secs_ms(secs):
    return None if secs is None else round(secs * 1000)


def emit(item, part, session, at, **fields):
    print(json.dumps({"type": item, "part": part, "session": session, "at": at, "fields": fields}))


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
        self.raised: list = []  # each callout: it, part, session, frame, keys, pit lane
        self.outcome: dict[int, str] = {}  # id(callout): held or dropped in the pit lane

    def session(self, store: SessionStore) -> int:
        s = store.session
        return 0 if s is None else self.sessions.get(s.session_id, 0)

    def frame_at(self, state) -> int:
        return self.seen.get(id(state), self.frame)

    def replay(self, path: Path, last: bool) -> None:
        self.part += 1
        part = self.part
        states = list(read_recording(path, strict=True))
        self.frame_of = {}
        for n, s in enumerate(states, 1):
            self.frame_of.setdefault((s.packet_id, s.t), []).append(n)
        self.seen = {}
        store = SessionStore(self.store_dir)
        cfg = copy.deepcopy(DEFAULT_RULES)
        validate_templates(cfg)
        rules = build_rules(cfg["rules"])
        engine = Engine(rules, CalloutQueue(max_age=None), store)
        self.wrap_engine(engine, store, part)
        self.wrap_store(engine, store, part)
        for rule in rules:
            self.wrap_rule(rule, store, part)
        try:
            for state in states:
                engine.process(state)
                while engine.queue.pop(state.t) is not None:
                    pass
            # A recording before the last ends as the engineer stopping: the next
            # one may continue its session (#273).
            engine.close_session(shutdown=not last)
        finally:
            store.close()

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
            return apply_pits(status, state)

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
                    self.outcome[id(c)] = {"hold": "held", "drop": "dropped"}[engine._policy(c)]
            return out

        engine._quiet = _quiet

    def wrap_store(self, engine: Engine, store: SessionStore, part: int) -> None:
        start = store.start_session

        def start_session(state, **kw):
            session = start(state, **kw)
            if session.session_id in self.sessions:
                emit("session.continued", part, self.sessions[session.session_id], self.frame)
            else:
                self.sessions[session.session_id] = len(self.sessions) + 1
                emit(
                    "session.started",
                    part,
                    self.sessions[session.session_id],
                    self.frame,
                    car_code=state.car_code,
                    total_laps=state.total_laps or None,
                    joined_on_lap=session.joined_on_lap,
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
                emit("session.ended", part, session, self.frame_at(state), finished=summary["finished"])
            return out

        store.end_session = end_session

    def wrap_rule(self, rule, store: SessionStore, part: int) -> None:
        """Each callout with the phrase keys its rule picked for it: the first is its
        kind, the script key v2 publishes (paddock#71)."""
        keys: list[str] = []
        pick, first = rule.phrases.pick, rule.phrases.first

        def _pick(key, default):
            keys.append(key)
            return pick(key, default)

        def _first(options, default):
            n = len(keys)
            out = first(options, default)
            if len(keys) == n and options:
                keys.append(options[-1])  # none configured: the general key's default
            return out

        rule.phrases.pick, rule.phrases.first = _pick, _first
        make = rule.callout

        def callout(text, state, priority=None):
            c = make(text, state, priority)
            picked = list(keys)
            keys.clear()
            self.raised.append((c, part, self.session(store), self.frame, picked, rule.pit_lane))
            return c

        rule.callout = callout

    def write_callouts(self) -> None:
        """Each callout raised, after the run, when v1's pit lane has had its say."""
        for c, part, session, at, picked, pit_lane in self.raised:
            emit(
                "callout",
                part,
                session,
                at,
                rule=c.rule,
                kind=picked[0] if picked else "",
                priority=c.priority,
                pit_lane=pit_lane,
                slots=None,
                text=c.text,
                outcome=self.outcome.get(id(c), "spoken"),
            )


def main(paths: list[Path]) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        run = Run(Path(tmp))
        for n, path in enumerate(paths, 1):
            run.replay(path, last=n == len(paths))
    run.write_callouts()


if __name__ == "__main__":
    main([Path(p) for p in sys.argv[1:]])
