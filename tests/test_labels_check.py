"""The labels check (scripts/labels_check.py): docs/labels.md and .github/labels.yml
list the same labels with the same colours."""

from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

_spec = importlib.util.spec_from_file_location("labels_check", ROOT / "scripts" / "labels_check.py")
assert _spec is not None and _spec.loader is not None
labels_check = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(labels_check)

DOC = """# Labels

## Phase

| Label | Colour | Meaning | Applied by |
|---|---|---|---|
| `parity` | `#1d76db` | Parity. | Author. |
| `component: org` | `#C5DEF5` | The organisation. | Maintainer. |

## To be removed

| Label | Why it goes |
|---|---|
| `wontfix` | Closing records it. |
"""

DATA = """# A comment.
- name: parity
  color: 1d76db
  description: Needed for parity with v1

- name: "component: org"
  color: c5def5
  description: "The organisation: .github"
"""


class ThisRepository(unittest.TestCase):
    def test_the_two_files_agree(self):
        self.assertEqual(labels_check.check(), [])

    def test_every_label_on_the_page_is_read(self):
        labels, removed = labels_check.from_doc((ROOT / labels_check.DOC).read_text())
        self.assertIn("needs triage", labels)
        self.assertIn("component: paddock", labels)
        self.assertIn("wontfix", removed)


class Cases(unittest.TestCase):
    def check(self, doc: str = DOC, data: str = DATA) -> list[str]:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for path, text in ((labels_check.DOC, doc), (labels_check.DATA, data)):
                (root / path).parent.mkdir(parents=True, exist_ok=True)
                (root / path).write_text(text)
            return labels_check.check(root)

    def test_matching_files_pass(self):
        self.assertEqual(self.check(), [])

    def test_a_label_missing_from_the_data_fails(self):
        self.assertEqual(
            self.check(data=DATA.split("\n\n")[0] + "\n"),
            ["'component: org' is in docs/labels.md but not in .github/labels.yml"],
        )

    def test_a_label_missing_from_the_page_fails(self):
        self.assertEqual(
            self.check(data=DATA + "- name: hold\n  color: d93f0b\n"),
            ["'hold' is in .github/labels.yml but not in docs/labels.md"],
        )

    def test_a_label_to_be_removed_fails_in_the_data(self):
        self.assertEqual(
            self.check(data=DATA + "- name: wontfix\n  color: ffffff\n"),
            ["'wontfix' is in .github/labels.yml but docs/labels.md lists it under 'To be removed'"],
        )

    def test_a_different_colour_fails(self):
        self.assertEqual(
            self.check(data=DATA.replace("1d76db", "0e8a16")),
            ["'parity' is #1d76db in docs/labels.md but 0e8a16 in .github/labels.yml"],
        )

    def test_a_line_it_cannot_read_fails(self):
        problems = self.check(data=DATA + "- {name: x, color: ffffff}\n")
        self.assertEqual(len(problems), 1)
        self.assertIn("not a name, colour or description", problems[0])

    def test_a_label_without_a_colour_fails(self):
        self.assertIn(
            ".github/labels.yml: 'parity' has no colour",
            self.check(data=DATA.replace("  color: 1d76db\n", "")),
        )

    def test_a_repeated_label_fails(self):
        problems = self.check(data=DATA + "- name: parity\n  color: 1d76db\n")
        self.assertTrue(any("listed twice" in p for p in problems), problems)


if __name__ == "__main__":
    unittest.main()
