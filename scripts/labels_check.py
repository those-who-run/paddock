"""Check that docs/labels.md and .github/labels.yml list the same labels (#3 R3).

``docs/labels.md`` says what each label means; ``.github/labels.yml`` holds the same
list as data, for the maintainer to apply. Both are edited by hand, so this checks
that they agree: the same names, each with the same colour, and that no label in
the page's "To be removed" table is still in ``labels.yml``. The descriptions differ
on purpose (the page explains, the file has GitHub's short text), so they are not
compared.

``make test``, CI's ``test`` job and the ``labels`` commit hook run it. Standard
library only, so ``labels.yml`` is read line by line: a line that is not a comment,
a blank, ``- name:``, ``color:`` or ``description:`` fails the check rather than
being skipped.

    python3 scripts/labels_check.py
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DOC = Path("docs/labels.md")
DATA = Path(".github/labels.yml")

# The page's tables: `| `name` | `#rrggbb` | ...`. The "To be removed" table has
# no colour column.
SECTION = re.compile(r"^##\s+(.*?)\s*$")
ROW = re.compile(r"^\|\s*`([^`]+)`\s*\|\s*`#([0-9a-fA-F]{6})`\s*\|")
REMOVED_ROW = re.compile(r"^\|\s*`([^`]+)`\s*\|")
REMOVED = "To be removed"

NAME = re.compile(r"""^-\s+name:\s*(?:"([^"]+)"|'([^']+)'|(.+?))\s*$""")
COLOR = re.compile(r"""^\s+color:\s*["']?([0-9a-fA-F]{6})["']?\s*$""")
DESCRIPTION = re.compile(r"^\s+description:\s*\S.*$")


def from_doc(text: str) -> tuple[dict[str, str], set[str]]:
    """The page's labels with their colours, and the names it lists for removal."""
    labels: dict[str, str] = {}
    removed: set[str] = set()
    section = ""
    for line in text.splitlines():
        if m := SECTION.match(line):
            section = m.group(1)
        elif section == REMOVED:
            if (m := REMOVED_ROW.match(line)) and m.group(1) != "Label":
                removed.add(m.group(1))
        elif m := ROW.match(line):
            labels[m.group(1)] = m.group(2).lower()
    return labels, removed


def from_data(text: str) -> tuple[dict[str, str], list[str]]:
    """``labels.yml``'s labels with their colours, and what could not be read."""
    labels: dict[str, str] = {}
    problems: list[str] = []
    name = None
    for n, line in enumerate(text.splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if m := NAME.match(line):
            name = next(g for g in m.groups() if g is not None)
            if name in labels:
                problems.append(f"{DATA}:{n}: {name!r} is listed twice")
            labels[name] = ""
        elif (m := COLOR.match(line)) and name is not None:
            labels[name] = m.group(1).lower()
        elif DESCRIPTION.match(line) and name is not None:
            continue
        else:
            problems.append(f"{DATA}:{n}: not a name, colour or description: {line.strip()!r}")
    problems += [f"{DATA}: {k!r} has no colour" for k, v in labels.items() if not v]
    return labels, problems


def check(root: Path = ROOT) -> list[str]:
    """What differs between the two files, one message each."""
    doc, removed = from_doc((root / DOC).read_text(encoding="utf-8"))
    data, problems = from_data((root / DATA).read_text(encoding="utf-8"))
    if not doc:
        problems.append(f"{DOC}: no label rows found")
    for name in sorted(doc.keys() - data.keys()):
        problems.append(f"{name!r} is in {DOC} but not in {DATA}")
    for name in sorted(data.keys() - doc.keys()):
        if name in removed:
            problems.append(f"{name!r} is in {DATA} but {DOC} lists it under {REMOVED!r}")
        else:
            problems.append(f"{name!r} is in {DATA} but not in {DOC}")
    for name in sorted(doc.keys() & data.keys()):
        if data[name] and doc[name] != data[name]:
            problems.append(f"{name!r} is #{doc[name]} in {DOC} but {data[name]} in {DATA}")
    return problems


def main(root: Path = ROOT) -> int:
    problems = check(root)
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"{len(problems)} difference(s): make {DOC} and {DATA} agree", file=sys.stderr)
        return 1
    print(f"labels agree in {DOC} and {DATA}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
