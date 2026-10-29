"""The callout kinds parity/v1events.py gives v1's callouts (paddock#104): the kind
contracts v0.2.3 names for each rule (Callout's comment, "The kinds ecu sends, by
rule"), so ecu's first callouts are compared with v1's on the contract's terms.

Each rule is checked against contracts' callout example for it
(parity/testdata/contracts/, v0.2.3), with the phrase keys v1's rule picks for that
callout, in the order it picks them (v1.0.0's rules/*.py). The module's v1 imports
are inside the replay, so it loads without v1."""

from __future__ import annotations

import importlib.util
import json
import unittest
from pathlib import Path

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


if __name__ == "__main__":
    unittest.main()
