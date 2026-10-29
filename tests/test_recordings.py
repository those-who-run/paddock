"""The shared recordings (recordings/, paddock#39 and #40): every file is copied from v1
at v1.0.0 and listed in recordings/SOURCES.yaml with its v1 path, its sha256, its frame
count and a link to its entry in v1's FIXTURES.yaml, and every entry has its file. The
recordings the compose stack's replay and the parity harness name are among them.

Standard library only, as paddock's other checks are. SOURCES.yaml is the subset of
YAML it says it is, read line by line; anything else in it fails, so the index can't
drift into YAML this reader (or the harness's, parity/recordings.go) would misread."""

from __future__ import annotations

import gzip
import hashlib
import re
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RECORDINGS = ROOT / "recordings"

KEYS = ["entry", "file", "frames", "sha256", "v1"]
ENTRY_LINK = re.compile(
    r"^https://github\.com/those-who-run/those-who-run/blob/v1\.0\.0/tests/fixtures/FIXTURES\.yaml#L\d+-L\d+$"
)
# #39's CI replay and #40's three default recordings (R4).
NAMED = [
    "real-race-2laps-2026-10-14.jsonl.gz",
    "real-nuc-suzuka-no-stop-2026-10-15.jsonl.gz",
    "real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz",
    "real-nuc-sugo-restarts-2026-10-18.jsonl.gz",
]


def read_sources(text: str) -> list[dict[str, str]]:
    """The entries of SOURCES.yaml: a list of `key: value` entries under `fixtures:`."""
    entries: list[dict[str, str]] = []
    in_list = False
    for n, line in enumerate(text.rstrip("\n").split("\n"), 1):
        where = f"SOURCES.yaml:{n}"
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if line == "fixtures:" and not in_list:
            in_list = True
            continue
        if not in_list:
            raise ValueError(f"{where}: {line!r} before `fixtures:`")
        if line.startswith("  - "):
            entries.append({})
            item = line[4:]
        elif line.startswith("    ") and entries:
            item = line[4:]
        else:
            raise ValueError(f"{where}: {line!r} is not an entry's line")
        key, sep, value = item.partition(": ")
        if not sep or " " in key or ":" in key or not value:
            raise ValueError(f"{where}: {line!r} is not `key: value`")
        if key in entries[-1]:
            raise ValueError(f"{where}: {key} twice in one entry")
        entries[-1][key] = value
    return entries


def check(directory: Path) -> list[str]:
    """Every way the files in directory and its SOURCES.yaml disagree."""
    try:
        entries = read_sources((directory / "SOURCES.yaml").read_text(encoding="utf-8"))
    except (OSError, ValueError) as err:
        return [str(err)]
    errors: list[str] = []
    listed: set[str] = set()
    for entry in entries:
        if sorted(entry) != KEYS:
            errors.append(f"{entry.get('file')}: keys {sorted(entry)}, want {KEYS}")
            continue
        name = entry["file"]
        if name in listed:
            errors.append(f"{name}: listed twice")
        listed.add(name)
        if entry["v1"] != f"tests/fixtures/{name}":
            errors.append(f"{name}: v1 path {entry['v1']}, want the same name under tests/fixtures/")
        if not ENTRY_LINK.match(entry["entry"]):
            errors.append(f"{name}: entry {entry['entry']} is not a link to v1's FIXTURES.yaml at v1.0.0")
        try:
            data = (directory / name).read_bytes()
        except OSError as err:
            errors.append(f"{name}: listed, but {err}")
            continue
        if (digest := hashlib.sha256(data).hexdigest()) != entry["sha256"]:
            errors.append(f"{name}: sha256 {digest}, SOURCES.yaml says {entry['sha256']}")
        try:
            lines = gzip.decompress(data).count(b"\n")
        except (OSError, EOFError) as err:
            errors.append(f"{name}: {err}")
            continue
        if str(lines) != entry["frames"]:
            errors.append(f"{name}: {lines} lines, SOURCES.yaml says {entry['frames']} frames")
    for path in sorted(directory.glob("*.jsonl.gz")):
        if path.name not in listed:
            errors.append(f"{path.name}: not in SOURCES.yaml")
    return errors


class RecordingsTest(unittest.TestCase):
    def test_every_file_matches_its_entry(self) -> None:
        self.assertEqual(check(RECORDINGS), [])

    def test_the_named_recordings_are_listed(self) -> None:
        entries = read_sources((RECORDINGS / "SOURCES.yaml").read_text(encoding="utf-8"))
        names = {e["file"] for e in entries}
        for name in NAMED:
            self.assertIn(name, names)


class CheckTest(unittest.TestCase):
    """The check fails each way a file and its entry can disagree."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        self.data = gzip.compress(b'{"t":1}\n{"t":2}\n')
        (self.dir / "a.jsonl.gz").write_bytes(self.data)

    def write(self, **changes: str) -> None:
        entry = {
            "file": "a.jsonl.gz",
            "v1": "tests/fixtures/a.jsonl.gz",
            "sha256": hashlib.sha256(self.data).hexdigest(),
            "frames": "2",
            "entry": "https://github.com/those-who-run/those-who-run/blob/v1.0.0/tests/fixtures/FIXTURES.yaml#L1-L9",
        } | changes
        lines = [f"    {k}: {v}" for k, v in entry.items()]
        lines[0] = "  - " + lines[0].lstrip()
        (self.dir / "SOURCES.yaml").write_text("fixtures:\n" + "\n".join(lines) + "\n", encoding="utf-8")

    def test_a_good_entry_passes(self) -> None:
        self.write()
        self.assertEqual(check(self.dir), [])

    def test_a_changed_file_fails(self) -> None:
        self.write(sha256="0" * 64)
        self.assertIn("sha256", " ".join(check(self.dir)))

    def test_a_wrong_frame_count_fails(self) -> None:
        self.write(frames="3")
        self.assertIn("2 lines", " ".join(check(self.dir)))

    def test_a_file_not_listed_fails(self) -> None:
        self.write()
        (self.dir / "b.jsonl.gz").write_bytes(self.data)
        self.assertEqual(check(self.dir), ["b.jsonl.gz: not in SOURCES.yaml"])

    def test_an_entry_with_no_file_fails(self) -> None:
        self.write()
        (self.dir / "a.jsonl.gz").unlink()
        self.assertIn("listed, but", " ".join(check(self.dir)))

    def test_a_link_off_v1_0_0_fails(self) -> None:
        self.write(entry="https://github.com/those-who-run/those-who-run/blob/main/tests/fixtures/FIXTURES.yaml#L1-L9")
        self.assertIn("not a link", " ".join(check(self.dir)))

    def test_another_v1_path_fails(self) -> None:
        self.write(v1="tests/fixtures/other.jsonl.gz")
        self.assertIn("v1 path", " ".join(check(self.dir)))

    def test_yaml_outside_the_subset_fails(self) -> None:
        (self.dir / "SOURCES.yaml").write_text("fixtures:\n  - file: [a, b]\n   x\n", encoding="utf-8")
        self.assertIn("not an entry's line", " ".join(check(self.dir)))


if __name__ == "__main__":
    unittest.main()
