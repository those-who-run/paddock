"""Check the JetStream definitions in deploy/nats/ against contracts' subjects (#36 R5).

A server refuses a stream whose subjects overlap another's, but only when the
bootstrap reaches it; and a stream that captures nothing, or a subject that no
stream captures, is not an error to the server at all. So this reads the files and
checks, before anything runs:

- each stream file (``deploy/nats/streams/*.json``) names the stream after the
  file (``telemetry.json`` is ``TELEMETRY``) and sets its limits: storage,
  replicas, retention, a discard policy, a duplicate window, and ``max_age`` or
  ``max_bytes``;
- each stream file names every field the bootstrap compares, and only fields the
  server returns as written (``-1``, not ``0``, for no limit), since a deleted
  field would keep its old value on the server and a rewritten one would make
  every run edit the stream;
- each bucket file (``deploy/nats/kv/*.json``) names the bucket after the file and
  sets its description, history, storage, replicas, ``max_value_size`` and
  ``max_bucket_size``, and nothing else;
- every stream subject is a valid NATS subject filter, and no two of them overlap
  (a subject both would capture), in one stream or across two;
- every stream subject covers at least one subject in contracts' ``subjects.yaml``;
- every stream ``subjects.yaml`` names has a file, every subject there is
  captured whole by the stream its entry names and by no other, and a subject that
  names none is captured by no stream;
- every KV bucket ``subjects.yaml`` names has a file.

``subjects.yaml`` is a copy of contracts' at the tag below, kept in
``deploy/nats/`` so the check runs offline; the check fails if the copy is not that
file. A bump copies the file from the new tag and changes both constants.

``make test``, CI's ``test`` job and the ``nats`` commit hook run it. Standard
library only, so ``subjects.yaml`` is read line by line: a line in an entry that is
not ``key: value`` fails the check rather than being skipped.

    python3 scripts/nats_check.py
"""

from __future__ import annotations

import hashlib
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
NATS = Path("deploy/nats")

CONTRACTS_TAG = "v0.2.3"
SUBJECTS_SHA256 = "eb4aaed845c5e4c5d39087a783c4710de2b887610522dafee86ad7cc5344a798"

# The bootstrap compares only the fields a file names, so a field deleted from a
# file would keep its old value on the server, unnoticed. Every stream file names
# all of these, and nothing else but STREAM_OPTIONAL: fields the server returns as
# they were written. A field it leaves out or rewrites would differ on every run,
# and the bootstrap would edit the stream every time.
STREAM_FIELDS = (
    "name", "description", "subjects", "retention", "storage", "num_replicas", "discard",
    "max_age", "max_bytes", "duplicate_window", "max_msgs", "max_msgs_per_subject",
    "max_msg_size", "max_consumers", "deny_delete", "deny_purge", "allow_rollup_hdrs",
    "allow_direct",
)  # fmt: skip
# sealed is left out: the server refuses a stream created sealed.
STREAM_OPTIONAL = ("mirror_direct",)
STREAM_CHOICES = {
    "retention": ("limits", "interest", "workqueue"),
    "storage": ("file", "memory"),
    "discard": ("old", "new"),
}
# The server stores 0 for these as -1 (unlimited), so they are -1 or a positive limit.
STREAM_LIMITS = ("max_bytes", "max_msgs", "max_msgs_per_subject", "max_msg_size", "max_consumers")
STREAM_FLAGS = ("deny_delete", "deny_purge", "allow_rollup_hdrs", "allow_direct", *STREAM_OPTIONAL)
# The same for a bucket, whose fields bootstrap.sh maps to nats kv add's flags.
BUCKET_FIELDS = ("bucket", "description", "history", "storage", "replicas", "max_value_size", "max_bucket_size")

SECTION = re.compile(r"^([a-z_]+):\s*$")
ITEM = re.compile(r"^  - ([a-z_]+):\s*(.*?)\s*$")
FIELD = re.compile(r"^    ([a-z_]+):\s*(.*?)\s*$")
TOKEN = re.compile(r"^[^\s.*>]+$")


def tokens(subject: str) -> list[str]:
    """A subject's tokens; a ``<placeholder>`` token stands for any one token."""
    return ["*" if "<" in t else t for t in subject.split(".")]


def subject_problem(subject: str) -> str | None:
    """Why ``subject`` is not a valid stream subject filter, or None."""
    parts = subject.split(".")
    for i, t in enumerate(parts):
        if t == ">" and i != len(parts) - 1:
            return "> is only allowed as the last token"
        if t not in ("*", ">") and not TOKEN.match(t):
            return f"token {t!r} is empty or holds white space, * or >"
    return None


def overlaps(a: list[str], b: list[str]) -> bool:
    """Whether some subject matches both filters."""
    for i in range(max(len(a), len(b))):
        if i >= len(a) or i >= len(b):
            return False
        x, y = a[i], b[i]
        if x == ">" or y == ">":
            return True
        if x != "*" and y != "*" and x != y:
            return False
    return True


def covers(f: list[str], p: list[str]) -> bool:
    """Whether every subject that matches ``p`` also matches the filter ``f``."""
    for i, ft in enumerate(f):
        if ft == ">":
            return len(p) > i
        if i >= len(p) or p[i] == ">":
            return False
        if ft != "*" and ft != p[i]:
            return False
    return len(p) == len(f)


def read_subjects(text: str) -> tuple[list[dict[str, str]], list[str]]:
    """The entries under ``subjects:``, as their scalar fields, and what could not be read."""
    entries: list[dict[str, str]] = []
    problems: list[str] = []
    section = ""
    for n, line in enumerate(text.splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if m := SECTION.match(line):
            section = m.group(1)
        elif section != "subjects":
            continue
        elif m := ITEM.match(line):
            entries.append({m.group(1): m.group(2).strip("\"'")})
        elif (m := FIELD.match(line)) and entries:
            entries[-1][m.group(1)] = m.group(2).strip("\"'")
        else:
            problems.append(f"subjects.yaml:{n}: not a `key: value` line in an entry: {line!r}")
    return entries, problems


def is_int(v: object) -> bool:
    return isinstance(v, int) and not isinstance(v, bool)


def stream_problems(rel: Path, s: dict) -> list[str]:
    """What in one stream file is missing, or written as the server would not return it."""
    problems = [f"{rel}: {f} is not set" for f in STREAM_FIELDS if f not in s]
    for f in s:
        if not f.startswith("$") and f not in STREAM_FIELDS + STREAM_OPTIONAL:
            problems.append(
                f"{rel}: {f} is not a field the check knows the server returns as written; "
                "add it to nats_check.py once it does"
            )
    for f, choices in STREAM_CHOICES.items():
        if f in s and s[f] not in choices:
            problems.append(f"{rel}: {f} is {s[f]!r}, not one of {', '.join(choices)}")
    if "description" in s and not (isinstance(s["description"], str) and s["description"]):
        problems.append(f"{rel}: description is empty, which the server leaves out")
    if "num_replicas" in s and not (is_int(s["num_replicas"]) and 1 <= s["num_replicas"] <= 5):
        problems.append(f"{rel}: num_replicas is not 1 to 5")
    for f in STREAM_LIMITS:
        if f in s and not (is_int(s[f]) and (s[f] == -1 or s[f] > 0)):
            problems.append(f"{rel}: {f} is {s[f]!r}: write -1 for no limit (the server stores 0 as -1)")
    for f in STREAM_FLAGS:
        if f in s and not isinstance(s[f], bool):
            problems.append(f"{rel}: {f} is not true or false")
    max_age, window = s.get("max_age", 0), s.get("duplicate_window", 0)
    if not (is_int(max_age) and max_age >= 0):
        problems.append(f"{rel}: max_age is not a number of nanoseconds")
    if not (is_int(window) and window > 0):
        problems.append(f"{rel}: duplicate_window is not a positive duration")
    elif is_int(max_age) and 0 < max_age < window:
        problems.append(f"{rel}: duplicate_window is longer than max_age, which the server refuses")
    if not ((is_int(max_age) and max_age > 0) or (is_int(s.get("max_bytes")) and s["max_bytes"] > 0)):
        problems.append(f"{rel}: neither max_age nor max_bytes is set: the stream could fill the disk")
    return problems


def bucket_problems(rel: Path, b: dict) -> list[str]:
    """What in one bucket file is missing, unknown or out of range."""
    problems = [f"{rel}: {f} is not set" for f in BUCKET_FIELDS if f not in b]
    for f in b:
        if not f.startswith("$") and f not in BUCKET_FIELDS:
            problems.append(f"{rel}: {f} is not a field bootstrap.sh knows for a bucket")
    if "description" in b and not (isinstance(b["description"], str) and b["description"]):
        problems.append(f"{rel}: description is empty, which the server leaves out")
    if "storage" in b and b["storage"] not in ("file", "memory"):
        problems.append(f"{rel}: storage is {b['storage']!r}, not file or memory")
    if "history" in b and not (is_int(b["history"]) and 1 <= b["history"] <= 64):
        problems.append(f"{rel}: history is not 1 to 64")
    if "replicas" in b and not (is_int(b["replicas"]) and 1 <= b["replicas"] <= 5):
        problems.append(f"{rel}: replicas is not 1 to 5")
    for f in ("max_value_size", "max_bucket_size"):
        if not (is_int(b.get(f)) and b[f] > 0):
            problems.append(f"{rel}: {f} is not a positive number of bytes")
    return problems


def load(path: Path, problems: list[str]) -> dict | None:
    try:
        data = json.loads(path.read_text())
    except (OSError, ValueError) as e:
        problems.append(f"{path}: {e}")
        return None
    if not isinstance(data, dict):
        problems.append(f"{path}: not a JSON object")
        return None
    return data


def check(root: Path = ROOT) -> tuple[list[str], str]:
    """Every problem with the definitions under ``root``, and a summary line."""
    problems: list[str] = []
    nats = root / NATS

    subjects_file = nats / "subjects.yaml"
    raw = subjects_file.read_bytes()
    if hashlib.sha256(raw).hexdigest() != SUBJECTS_SHA256:
        problems.append(
            f"{NATS}/subjects.yaml is not contracts' subjects.yaml at {CONTRACTS_TAG}: "
            "copy it from that tag, or bump CONTRACTS_TAG and SUBJECTS_SHA256 with it"
        )
    entries, unread = read_subjects(raw.decode())
    problems += unread

    # (stream, subject filter as written, its tokens)
    filters: list[tuple[str, str, list[str]]] = []
    stream_files = sorted((nats / "streams").glob("*.json"))
    for path in stream_files:
        rel = path.relative_to(root)
        s = load(path, problems)
        if s is None:
            continue
        name = s.get("name")
        if name != path.stem.upper():
            problems.append(f"{rel}: name is {name!r}, not {path.stem.upper()!r} after the file")
        problems += stream_problems(rel, s)
        subjects = s.get("subjects")
        if not isinstance(subjects, list) or not subjects:
            problems.append(f"{rel}: subjects is not a list of subjects")
            continue
        for subject in subjects:
            if why := subject_problem(subject):
                problems.append(f"{rel}: subject {subject!r}: {why}")
            else:
                filters.append((str(name), subject, subject.split(".")))

    for i, (sa, fa, ta) in enumerate(filters):
        for sb, fb, tb in filters[i + 1 :]:
            if overlaps(ta, tb):
                problems.append(f"streams {sa} ({fa}) and {sb} ({fb}) overlap: a subject would match both")

    streams = {s for s, _, _ in filters}
    patterns = [e for e in entries if "pattern" in e]
    for stream, f, t in filters:
        if not any(overlaps(t, tokens(e["pattern"])) for e in patterns):
            problems.append(f"stream {stream}'s subject {f} covers no subject in subjects.yaml")
    for e in patterns:
        p = tokens(e["pattern"])
        home = e.get("stream", "")
        for stream, f, t in filters:
            if stream == home:
                continue
            if overlaps(t, p):
                problems.append(
                    f"{e['pattern']} goes to {home or 'no stream'} in subjects.yaml, but {stream} ({f}) captures it"
                )
        if home and home not in streams:
            problems.append(f"{e['pattern']} goes to {home} in subjects.yaml, which has no file in {NATS}/streams")
        elif home and not any(s == home and covers(t, p) for s, _, t in filters):
            problems.append(f"{e['pattern']} goes to {home} in subjects.yaml, but no subject of {home} covers all of it")

    bucket_files = sorted((nats / "kv").glob("*.json"))
    buckets = set()
    for path in bucket_files:
        rel = path.relative_to(root)
        b = load(path, problems)
        if b is None:
            continue
        buckets.add(b.get("bucket"))
        if b.get("bucket") != path.stem:
            problems.append(f"{rel}: bucket is {b.get('bucket')!r}, not {path.stem!r} after the file")
        problems += bucket_problems(rel, b)
    for e in entries:
        if "kv" in e and e["kv"] not in buckets:
            problems.append(f"subjects.yaml names KV bucket {e['kv']}, which has no file in {NATS}/kv")

    summary = (
        f"{len(stream_files)} streams, {len(bucket_files)} buckets, "
        f"{len(patterns)} subjects from contracts {CONTRACTS_TAG}"
    )
    return problems, summary


def main() -> int:
    problems, summary = check()
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"nats definitions: {len(problems)} problems", file=sys.stderr)
        return 1
    print(f"nats definitions: {summary}, no overlaps")
    return 0


if __name__ == "__main__":
    sys.exit(main())
