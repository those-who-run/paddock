"""The callout kinds parity/v1events.py gives v1's callouts (paddock#104): the kind
contracts v0.2.3 names for each rule (Callout's comment, "The kinds ecu sends, by
rule"), so ecu's first callouts are compared with v1's on the contract's terms.

Each rule is checked against contracts' callout example for it
(parity/testdata/contracts/, v0.2.3), with the phrase keys v1's rule picks for that
callout, in the order it picks them (v1.0.0's rules/*.py). The module's v1 imports
are inside the replay, so it loads without v1.

The slots are checked the same way, on the contract's examples: what v1's rule put
in the phrases it picked, left out where ecu's protojson leaves a slot out.

The session summary is checked against contracts' session.ended example: the
values v1's summary.json gives (paddock#86 AC1), in SessionSummary's names, with
the two v1 never stored derived as SessionSummary defines them."""

from __future__ import annotations

import importlib.util
import json
import unittest
from pathlib import Path
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parent.parent
EXAMPLES = ROOT / "parity" / "testdata" / "contracts"

spec = importlib.util.spec_from_file_location("v1events", ROOT / "parity" / "v1events.py")
assert spec is not None and spec.loader is not None
v1events = importlib.util.module_from_spec(spec)
spec.loader.exec_module(v1events)
kind = v1events.callout_kind


def example(name: str) -> dict:
    return json.loads((EXAMPLES / f"{name}.json").read_text())


class TheContractsExamples(unittest.TestCase):
    def check(self, name: str, keys: list[str], briefed: bool = False) -> None:
        ex = example(name)
        self.assertEqual(kind(ex["rule"], keys, briefed), ex["kind"], name)

    def test_lap_time(self) -> None:
        # The {delta} key, then the template; a new best's own template first.
        self.check("callout_lap_time", ["delta_slower", "template"])
        self.check("callout_new_best", ["template_new_best", "delta_faster"])

    def test_position(self) -> None:
        self.check("callout_position", ["template_lost"])

    def test_race_summary(self) -> None:
        # The {position} key, then the template and its {best}.
        self.check("callout_race_summary", ["template_position_podium", "template", "template_best"])

    def test_start_brief(self) -> None:
        self.check("callout_start_brief", ["template_track", "template_laps", "template_position"])

    def test_telemetry(self) -> None:
        self.check("callout_telemetry", ["template_lost"])


class TheOtherKinds(unittest.TestCase):
    def test_start_brief(self) -> None:
        self.assertEqual(kind("start_brief", ["template_laps"], False), "template")
        self.assertEqual(kind("start_brief", ["template_track"], True), "template_track")
        self.assertEqual(kind("start_brief", ["template_joined", "template_joined_position"], False), "template_joined")

    def test_race_summary(self) -> None:
        self.assertEqual(kind("race_summary", ["template", "template_best"], False), "template")
        self.assertEqual(kind("race_summary", ["template_quit", "template_best"], False), "template_quit")
        self.assertEqual(kind("race_summary", ["template_position_win", "template"], False), "template_position_win")

    def test_the_first_key(self) -> None:
        self.assertEqual(kind("lap_time", ["delta_slower_small", "template"], False), "delta_slower_small")
        self.assertEqual(kind("fuel", ["template_pit_next"], False), "template_pit_next")
        self.assertEqual(kind("final_lap", ["template"], False), "template")
        self.assertEqual(kind("fuel", [], False), "")


class TheSlots(unittest.TestCase):
    def test_present_leaves_out_what_protojson_does(self) -> None:
        slots = {"lap": 3, "delta_ms": 0, "track": "", "timed": False, "corners": [], "laps": None}
        self.assertEqual(v1events.present(slots), {"lap": 3, "delta_ms": 0})

    def test_the_brief(self) -> None:
        # contracts' start brief example: the laps, then the place in the field.
        slots = example("callout_start_brief")["slots"]
        state = SimpleNamespace(total_laps=slots["laps"], position=slots["position"], total_cars=slots["totalCars"])
        got = v1events.brief_slots(["template_laps", "template_position"], [], state)
        self.assertEqual(got, {"laps": 6, "position": 10, "total_cars": 20})
        tracks = ["Sportsland SUGO"]
        self.assertEqual(v1events.brief_slots(["template_track"], tracks, state), {"track": "Sportsland SUGO"})
        self.assertEqual(tracks, [])

    def test_a_joined_brief(self) -> None:
        joined = SimpleNamespace(current_lap=5, total_laps=12, has_lap_count=True, position=4, total_cars=16)
        self.assertEqual(
            v1events.brief_slots(["template_joined", "template_joined_position"], [], joined),
            {"lap": 5, "laps": 12, "position": 4, "total_cars": 16},
        )
        timed = SimpleNamespace(current_lap=2, has_lap_count=False, laps_left=None)
        self.assertEqual(v1events.brief_slots(["template_joined", "template_timed"], [], timed), {"lap": 2, "timed": True})
        late = SimpleNamespace(laps_left=3)
        self.assertEqual(v1events.brief_slots(["template_laps_to_go"], [], late), {"laps_left": 3})


def v1_summary(**over) -> dict:
    """A summary.json as v1 wrote it, with what contracts' session.ended example
    holds: a 10-lap race won from the flag, best lap 4."""
    laps = [{"lap": n, "lap_time_ms": 97412 if n == 4 else 98000 + n, "complete": True} for n in range(1, 11)]
    out = {
        "started_at": 1767225614.0159851,
        "ended_at": 1767226006.1761720,
        "driving_ms": 390108,
        "paused_ms": 2052,
        "ended_paused": False,
        "session_kind": {"kind": "lap_race"},
        "car_code": 683,
        "car_name": "Honda Civic Si Group A version '87",
        "total_laps": 10,
        "laps_completed": 10,
        "best_lap_ms": 97412,
        "finished": True,
        "joined_on_lap": None,
        "final_position": 1,
        "total_cars": 2,
        "fuel_burned": 130.393,
        "fuel_burned_by_stint": [73.825, 56.568],
        "stints": 2,
        "pit_stops": [{"stop": 1}],
        "laps": laps,
    }
    out.update(over)
    return out


class TheSummary(unittest.TestCase):
    def test_the_contracts_example(self) -> None:
        ex = example("session_ended")["summary"]
        got = v1events.summary_fields(v1_summary())
        self.assertEqual(got["laps_completed"], ex["lapsCompleted"])
        self.assertEqual(got["best_lap"], ex["bestLap"])
        self.assertEqual(got["best_lap_ms"], ex["bestLapTimeMs"])
        self.assertEqual(got["finishing_position"], ex["finishingPosition"])
        self.assertEqual(got["final_position"], ex["finalPosition"])

    def test_the_values(self) -> None:
        got = v1events.summary_fields(v1_summary())
        self.assertEqual(got["started_at"], 1767225614016)  # epoch milliseconds, to the nearest
        self.assertEqual(got["ended_at"], 1767226006176)
        self.assertEqual(got["pit_stops"], 1)
        self.assertEqual(got["fuel_burned_by_stint"], [73.825, 56.568])
        self.assertNotIn("laps", got)  # parity laps compares them

    def test_the_best_lap_is_the_first_complete_lap_with_its_time(self) -> None:
        laps = [
            {"lap": 1, "lap_time_ms": 90000, "complete": False},
            {"lap": 2, "lap_time_ms": 90000, "complete": True},
            {"lap": 3, "lap_time_ms": 90000, "complete": True},
        ]
        self.assertEqual(v1events.summary_fields(v1_summary(best_lap_ms=90000, laps=laps))["best_lap"], 2)
        self.assertIsNone(v1events.summary_fields(v1_summary(best_lap_ms=None, laps=laps))["best_lap"])

    def test_a_finishing_position_only_for_a_lap_race_that_finished(self) -> None:
        self.assertIsNone(v1events.summary_fields(v1_summary(finished=False))["finishing_position"])
        timed = v1events.summary_fields(v1_summary(session_kind={"kind": "timed_race"}))
        self.assertIsNone(timed["finishing_position"])
        self.assertEqual(timed["final_position"], 1)
        self.assertIsNone(v1events.summary_fields(v1_summary(session_kind=None))["finishing_position"])


if __name__ == "__main__":
    unittest.main()
