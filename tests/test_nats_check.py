"""The NATS definitions check (scripts/nats_check.py, #36 R5): stream subjects match
contracts' subjects.yaml and never overlap, and every stream and bucket sets its
limits."""

from __future__ import annotations

import importlib.util
import json
import re
import shutil
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

_spec = importlib.util.spec_from_file_location("nats_check", ROOT / "scripts" / "nats_check.py")
assert _spec is not None and _spec.loader is not None
nats_check = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(nats_check)


def t(subject: str) -> list[str]:
    return nats_check.tokens(subject)


class Subjects(unittest.TestCase):
    def test_overlaps(self) -> None:
        self.assertTrue(nats_check.overlaps(t("twr.v1.*.evt.>"), t("twr.v1.parity.evt.>")))
        self.assertTrue(nats_check.overlaps(t("twr.v1.*.evt.>"), t("twr.v1.*.evt.>")))
        self.assertTrue(nats_check.overlaps(t("a.*"), t("*.b")))
        self.assertTrue(nats_check.overlaps(t("a.>"), t("a.b.c")))
        self.assertFalse(nats_check.overlaps(t("twr.v1.*.frame.gt7"), t("twr.v1.*.frame.gt7.lo")))
        self.assertFalse(nats_check.overlaps(t("twr.v1.*.frame.gt7"), t("twr.v1.*.evt.>")))
        self.assertFalse(nats_check.overlaps(t("a.>"), t("a")))
        self.assertFalse(nats_check.overlaps(t("a.*"), t("a.b.c")))

    def test_covers(self) -> None:
        self.assertTrue(nats_check.covers(t("twr.v1.*.frame.gt7"), t("twr.v1.<src>.frame.gt7")))
        self.assertTrue(nats_check.covers(t("twr.v1.*.evt.>"), t("twr.v1.<src>.evt.<sid>.lap.completed")))
        self.assertFalse(nats_check.covers(t("twr.v1.*.frame.gt7"), t("twr.v1.<src>.frame.gt7.lo")))
        self.assertFalse(nats_check.covers(t("twr.v1.live.frame.gt7"), t("twr.v1.<src>.frame.gt7")))
        self.assertFalse(nats_check.covers(t("twr.v1.*.evt.>"), t("twr.v1.<src>.evt")))
        self.assertFalse(nats_check.covers(t("a.*"), t("a.>")))

    def test_subject_problem(self) -> None:
        self.assertIsNone(nats_check.subject_problem("twr.v1.*.evt.>"))
        self.assertIsNotNone(nats_check.subject_problem("twr.>.evt"))
        self.assertIsNotNone(nats_check.subject_problem("twr..evt"))
        self.assertIsNotNone(nats_check.subject_problem("twr.v1 .evt"))

    def test_read_subjects(self) -> None:
        entries, problems = nats_check.read_subjects(
            "# c\nsubjects:\n  - pattern: a.<src>.b\n    stream: S\n\n  - kv: live\n    key: x\nhelpers:\n  - y\n"
        )
        self.assertEqual(problems, [])
        self.assertEqual(entries, [{"pattern": "a.<src>.b", "stream": "S"}, {"kv": "live", "key": "x"}])
        _, problems = nats_check.read_subjects("subjects:\n  - pattern: a\n   stream: S\n")
        self.assertEqual(len(problems), 1)


class Definitions(unittest.TestCase):
    """The check over a copy of deploy/nats/, changed one way per test."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)
        shutil.copytree(ROOT / "deploy" / "nats", self.root / "deploy" / "nats")
        self.nats = self.root / "deploy" / "nats"

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def edit(self, rel: str, **changes: object) -> None:
        path = self.nats / rel
        data = json.loads(path.read_text())
        for k, v in changes.items():
            if v is None:
                data.pop(k, None)
            else:
                data[k] = v
        path.write_text(json.dumps(data))

    def problems(self) -> list[str]:
        return nats_check.check(self.root)[0]

    def test_the_repository_passes(self) -> None:
        problems, summary = nats_check.check(ROOT)
        self.assertEqual(problems, [])
        self.assertIn("2 streams, 1 buckets", summary)

    def test_a_third_stream_on_the_events_subject_fails(self) -> None:
        # AC3: caught here, before the bootstrap reaches a server.
        events = json.loads((self.nats / "streams" / "events.json").read_text())
        events["name"] = "AUDIT"
        (self.nats / "streams" / "audit.json").write_text(json.dumps(events))
        problems = self.problems()
        self.assertIn("streams AUDIT (twr.v1.*.evt.>) and EVENTS (twr.v1.*.evt.>) overlap: a subject would match both", problems)

    def test_a_narrower_overlap_fails(self) -> None:
        self.edit("streams/telemetry.json", subjects=["twr.v1.*.frame.gt7", "twr.v1.parity.evt.>"])
        self.assertTrue(any("overlap" in p for p in self.problems()))

    def test_capturing_the_low_rate_copy_fails(self) -> None:
        # A stream on the 20 Hz copy alone captures nothing contracts lists, and
        # leaves frame.gt7 with no stream. AC4 checks the same against a server.
        self.edit("streams/telemetry.json", subjects=["twr.v1.*.frame.gt7.lo"])
        problems = self.problems()
        self.assertTrue(any("covers no subject" in p for p in problems), problems)
        self.assertTrue(any("no subject of TELEMETRY covers all of it" in p for p in problems), problems)

    def test_a_subject_in_the_wrong_stream_fails(self) -> None:
        self.edit("streams/telemetry.json", subjects=["twr.v1.*.frame.gt7", "twr.v1.*.evt.x.session.started"])
        self.assertTrue(any("goes to EVENTS in subjects.yaml, but TELEMETRY" in p for p in self.problems()))

    def test_a_stream_that_captures_nothing_fails(self) -> None:
        self.edit("streams/events.json", subjects=["twr.v1.*.evt.>", "twr.v2.>"])
        self.assertIn("stream EVENTS's subject twr.v2.> covers no subject in subjects.yaml", self.problems())

    def test_a_stream_without_limits_fails(self) -> None:
        self.edit("streams/events.json", max_age=None, max_bytes=-1, duplicate_window=None, discard=None)
        problems = self.problems()
        self.assertIn("deploy/nats/streams/events.json: discard is not set", problems)
        self.assertIn("deploy/nats/streams/events.json: duplicate_window is not set", problems)
        self.assertTrue(any("neither max_age nor max_bytes" in p for p in problems))

    def test_a_stream_named_apart_from_its_file_fails(self) -> None:
        self.edit("streams/events.json", name="EVENTS2")
        self.assertTrue(any("not 'EVENTS' after the file" in p for p in self.problems()))

    def test_a_bucket_without_limits_fails(self) -> None:
        self.edit("kv/live.json", max_bucket_size=None, max_value_size=-1)
        problems = self.problems()
        self.assertIn("deploy/nats/kv/live.json: max_bucket_size is not set", problems)
        self.assertIn("deploy/nats/kv/live.json: max_value_size is not a positive number of bytes", problems)

    def test_a_bucket_named_in_subjects_yaml_needs_a_file(self) -> None:
        (self.nats / "kv" / "live.json").unlink()
        path = self.nats / "subjects.yaml"
        path.write_text(path.read_text().replace("\nhelpers:", "\n  - kv: live\n    key: <src>.x\n\nhelpers:"))
        problems = self.problems()
        self.assertIn("subjects.yaml names KV bucket live, which has no file in deploy/nats/kv", problems)

    def test_a_changed_copy_of_subjects_yaml_fails(self) -> None:
        path = self.nats / "subjects.yaml"
        path.write_text(path.read_text() + "\n")
        self.assertTrue(any("is not contracts' subjects.yaml at v0.1.3" in p for p in self.problems()))

    def test_a_zero_the_server_stores_as_minus_one_fails(self) -> None:
        # The server would return -1, so every run would edit the stream.
        self.edit("streams/telemetry.json", max_msgs=0)
        self.assertIn(
            "deploy/nats/streams/telemetry.json: max_msgs is 0: write -1 for no limit (the server stores 0 as -1)",
            self.problems(),
        )

    def test_a_deleted_field_fails(self) -> None:
        # The bootstrap would keep the old value on the server and say unchanged.
        self.edit("streams/events.json", max_msgs_per_subject=None)
        self.assertIn("deploy/nats/streams/events.json: max_msgs_per_subject is not set", self.problems())
        self.edit("kv/live.json", description=None)
        self.assertIn("deploy/nats/kv/live.json: description is not set", self.problems())

    def test_a_field_the_server_leaves_out_or_rewrites_fails(self) -> None:
        self.edit("streams/events.json", compression="none", retention="Limits", description="", allow_direct=1)
        problems = self.problems()
        rel = "deploy/nats/streams/events.json"
        self.assertTrue(any(p.startswith(f"{rel}: compression is not a field the check knows") for p in problems))
        self.assertIn(f"{rel}: retention is 'Limits', not one of limits, interest, workqueue", problems)
        self.assertIn(f"{rel}: description is empty, which the server leaves out", problems)
        self.assertIn(f"{rel}: allow_direct is not true or false", problems)

    def test_sealed_is_refused(self) -> None:
        # The server refuses a stream created sealed, so no file may set it.
        self.edit("streams/events.json", sealed=False)
        self.assertTrue(
            any(p.startswith("deploy/nats/streams/events.json: sealed is not a field") for p in self.problems())
        )

    def test_a_duplicate_window_longer_than_max_age_fails(self) -> None:
        self.edit("streams/events.json", max_age=60_000_000_000)
        self.assertIn(
            "deploy/nats/streams/events.json: duplicate_window is longer than max_age, "
            "and the server would cut it to max_age",
            self.problems(),
        )

    def test_a_bucket_value_out_of_range_or_unknown_fails(self) -> None:
        self.edit("kv/live.json", history=0, ttl="1h", storage="disk")
        problems = self.problems()
        rel = "deploy/nats/kv/live.json"
        self.assertIn(f"{rel}: history is not 1 to 64", problems)
        self.assertIn(f"{rel}: ttl is not a field bootstrap.sh knows for a bucket", problems)
        self.assertIn(f"{rel}: storage is 'disk', not file or memory", problems)


class ImagePins(unittest.TestCase):
    """Renovate's custom manager (renovate.json) finds every pinned NATS image, and
    the README's example runs the image the integration test uses."""

    def refs(self, rel: str) -> list[tuple[str, str, str]]:
        config = json.loads((ROOT / "renovate.json").read_text())
        (manager,) = config["customManagers"]
        self.assertTrue(any(re.search(p.strip("/"), rel) for p in manager["managerFilePatterns"]), rel)
        text = (ROOT / rel).read_text()
        found = []
        for pattern in manager["matchStrings"]:
            rx = re.compile(pattern.replace("(?<", "(?P<"))
            found += [(m["depName"], m["currentValue"], m["currentDigest"]) for m in rx.finditer(text)]
        return found

    def test_the_manager_finds_both_images_in_the_runner(self) -> None:
        refs = self.refs("scripts/nats_it.sh")
        self.assertEqual([r[0] for r in refs], ["docker.io/natsio/nats-box", "docker.io/library/nats"])
        text = (ROOT / "scripts" / "nats_it.sh").read_text()
        self.assertEqual(len(re.findall(r"^NATS_(?:BOX|SERVER)=", text, re.M)), len(refs))

    def test_the_readme_runs_the_tested_nats_box(self) -> None:
        (box, *_) = self.refs("scripts/nats_it.sh")
        self.assertEqual(self.refs("deploy/nats/README.md"), [box])


if __name__ == "__main__":
    unittest.main()
