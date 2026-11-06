"""Check that paddock's copies of the agent files are the canonical ones (#20 R3).

The guard, the settings that run it, and the skills and agent briefs live once, in
those-who-run/.github's ``agent/``, and each repository copies them unchanged
(``agent/README.md`` there). This checks the copies here byte for byte against
``agent/`` at the commit paddock's CI pins for ``checks.yml``, so there is one pinned
``.github`` version to bump: Renovate's bump of that pin fails this check until the
copies are brought up to the same commit, in the same PR.

What is copied where is ``agent/copies.json`` at that commit: a JSON object from a
path in ``.github`` to a path under ``.claude/`` here. A commit from before the
manifest has none, and its copies are the two it always had:

- ``.claude/hooks/guard.py`` is ``agent/guard.py``;
- ``.claude/settings.json`` is ``agent/settings.json``.

With a manifest, every file it lists must be at the pinned commit and equal the file
at its destination here, and it must list those two. Either way:

- a tracked file under ``.claude/skills/twr-*/`` or at ``.claude/agents/twr-*.md``
  that is not a destination is a stale copy, and fails;
- ``.claude/guard.json``, paddock's own values, loads with the guard's own reader,
  since a config the guard can't read blocks every Bash command.

A manifest that can't be read, or that names a destination outside ``.claude/`` or a
path with ``..``, fails the check: a manifest this can't trust is never a pass.

It fetches the files from raw.githubusercontent.com over HTTPS, so it needs the
network, and it asks git which files are tracked. ``make test`` and CI's ``test`` job
run it.

    python3 scripts/guard_copy.py
"""

from __future__ import annotations

import importlib.util
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CI = Path(".github/workflows/ci.yml")
CANONICAL = "those-who-run/.github"
MANIFEST = "agent/copies.json"
# The copies of a commit with no manifest, and the two every manifest must list: a
# manifest that left them out would stop the guard itself being checked.
COPIES = {
    "agent/guard.py": Path(".claude/hooks/guard.py"),
    "agent/settings.json": Path(".claude/settings.json"),
}
CONFIG = Path(".claude/guard.json")
# Where a manifest's paths may point: from .github's agent/ into .claude/ here.
SOURCES = "agent"
DESTINATIONS = ".claude"
# The names kept for copies: anything below .claude/skills/twr-*/, and
# .claude/agents/twr-*.md. A tracked file with such a name is a copy the manifest
# lists, or a stale one: a skill or brief that .github dropped or renamed, which a
# session here would still load.
SKILLS = ".claude/skills"
AGENTS = ".claude/agents"
PREFIX = "twr-"

PIN = re.compile(
    r"^\s*uses:\s*" + re.escape(CANONICAL) + r"/\.github/workflows/checks\.yml@([0-9a-f]{40})\b",
    re.M,
)


def pinned_commit(ci_text: str) -> str:
    """The commit of those-who-run/.github that ci.yml calls checks.yml at."""
    pins = set(PIN.findall(ci_text))
    if len(pins) != 1:
        raise ValueError(
            f"{CI} should call {CANONICAL}'s checks.yml at one full commit SHA; found {sorted(pins) or 'none'}"
        )
    return pins.pop()


class FetchError(Exception):
    """A canonical file couldn't be fetched; the message says which and why."""


class NotFound(FetchError):
    """The fetch was answered with a 404: no such file at that commit, or no such commit."""


def fetch(commit: str, path: str) -> bytes:
    """A file of those-who-run/.github at a commit."""
    # Quoted, so a listed path holding `?` or `#` is asked for as that file (and is
    # not there) rather than cutting the URL short at a file that is.
    url = f"https://raw.githubusercontent.com/{CANONICAL}/{commit}/{urllib.parse.quote(path)}"
    try:
        with urllib.request.urlopen(url, timeout=30) as response:  # noqa: S310 (fixed https URL)
            return response.read()
    except (urllib.error.URLError, TimeoutError) as e:
        # A 404 is a file that is not at that commit, or a pin that names no commit
        # of .github; anything else is most likely the network. One line either way,
        # as the other problems are.
        if isinstance(e, urllib.error.HTTPError):
            e.close()  # it holds the response open
        reason = e.reason if isinstance(e, urllib.error.URLError) else e
        # A 404 has a type of its own: for the manifest the check reads it as a
        # commit from before the manifest, and no other failure may pass for that.
        kind = NotFound if isinstance(e, urllib.error.HTTPError) and e.code == 404 else FetchError
        raise kind(
            f"could not fetch {url} ({reason}): check that the checks.yml pin in {CI} "
            "is a commit of those-who-run/.github, and the network"
        ) from None


def path_problem(path: object, under: str) -> str | None:
    """Why a manifest's path can't be used, or None when it is a plain path below `under`."""
    if not isinstance(path, str) or not path:
        return "is not a path"
    # Checked on the text, before anything treats it as a path: a `..` or a leading
    # `/` would take a destination out of .claude/, or a fetch out of agent/.
    parts = path.split("/")
    if ".." in parts:
        return "has `..` in it"
    if path.startswith("/") or "\\" in path or any(ord(c) < 32 or ord(c) == 127 for c in path):
        return "is not a plain relative path with `/` between its parts"
    if "" in parts or "." in parts:
        return "has an empty or `.` part"
    if parts[0] != under or len(parts) < 2:
        return f"is not under {under}/"
    return None


def _no_repeats(pairs: list[tuple[str, object]]) -> dict[str, object]:
    """A JSON object's members, refusing a key given twice (json keeps the last)."""
    seen: dict[str, object] = {}
    for key, value in pairs:
        if key in seen:
            raise ValueError(f"lists {key} twice")
        seen[key] = value
    return seen


def manifest_copies(raw: bytes, commit: str) -> tuple[dict[str, Path], list[str]]:
    """A manifest's copies, source to destination, and what is wrong with it.

    Any problem makes the whole manifest unusable: the caller checks nothing against
    a manifest it can't trust, and reports the problems instead.
    """
    where = f"{CANONICAL}'s {MANIFEST} at {commit[:12]}, the commit {CI} pins,"
    try:
        listed = json.loads(raw.decode("utf-8"), object_pairs_hook=_no_repeats)
    except (ValueError, RecursionError) as e:  # bad UTF-8, bad JSON, a repeated key
        return {}, [f"{where} can't be read ({e}): it should be one JSON object, source path to destination path"]
    if not isinstance(listed, dict):
        return {}, [f"{where} is not a JSON object, source path to destination path"]

    problems = []
    copies: dict[str, Path] = {}
    sources: dict[str, str] = {}  # destination to the source that claimed it
    for source, destination in listed.items():
        if reason := path_problem(source, SOURCES):
            problems.append(f"{where} lists the source {source!r}, which {reason}")
            continue
        if reason := path_problem(destination, DESTINATIONS):
            problems.append(f"{where} sends {source} to {destination!r}, which {reason}")
            continue
        if destination == CONFIG.as_posix():
            problems.append(
                f"{where} sends {source} to {CONFIG}, which holds each repository's own values and is never a copy"
            )
            continue
        if destination in sources:
            problems.append(f"{where} sends both {sources[destination]} and {source} to {destination}")
            continue
        sources[destination] = source
        copies[source] = Path(destination)
    if not problems:
        problems = [
            f"{where} does not send {source} to {destination}: every repository copies the guard and its settings"
            for source, destination in COPIES.items()
            if copies.get(source) != destination
        ]
    return ({}, problems) if problems else (copies, [])


class TrackedError(Exception):
    """git couldn't say which files are tracked; the message says why."""


def tracked_files(root: Path) -> list[str]:
    """The files git tracks under .claude/skills/ and .claude/agents/ in `root`, from its root."""
    # `root` decides the repository, not a GIT_DIR or index left in the environment
    # by a hook this runs under.
    env = {k: v for k, v in os.environ.items() if k not in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE")}
    try:
        listed = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z", "--", SKILLS, AGENTS],
            check=True, capture_output=True, env=env,
        )
    except (OSError, subprocess.CalledProcessError) as e:
        reason = e.stderr.decode("utf-8", "replace").strip() if isinstance(e, subprocess.CalledProcessError) else e
        raise TrackedError(
            f"could not ask git which files under {SKILLS}/ and {AGENTS}/ are tracked ({reason}): "
            "the check for stale copies needs git and a checkout"
        ) from None
    return [name for name in listed.stdout.decode("utf-8", "surrogateescape").split("\0") if name]


def is_copy_name(path: str) -> bool:
    """Whether a tracked path has one of the names kept for copies."""
    folder, _, rest = path.rpartition("/")
    if folder == AGENTS:
        return rest.startswith(PREFIX) and rest.endswith(".md")
    below = path.removeprefix(SKILLS + "/")
    return below != path and below.startswith(PREFIX) and "/" in below


def config_problem(guard: Path, config: Path) -> str | None:
    """Why the guard can't read the config, or None when it can."""
    spec = importlib.util.spec_from_file_location("paddock_guard", guard)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules["paddock_guard"] = module  # dataclasses look their module up
    spec.loader.exec_module(module)
    try:
        module.load_config(str(config))
    except module.ConfigError as e:
        return f"{config}: the guard can't read it: {e}"
    return None


def _check(root: Path, get, tracked) -> tuple[list[str], int | None]:
    """The problems, and how many copies the manifest lists when the pin has one."""
    try:
        commit = pinned_commit((root / CI).read_text(encoding="utf-8"))
    except ValueError as e:
        return [str(e)], None

    # A 404 is a commit from before the manifest, whose copies are the guard and the
    # settings. A pin that names no commit at all gives a 404 here too, and is
    # reported by the fetch of the guard below, as it always was.
    try:
        raw = get(commit, MANIFEST)
    except NotFound:
        copies, listed = dict(COPIES), None
    except FetchError as e:
        return [str(e)], None
    else:
        copies, problems = manifest_copies(raw, commit)
        if problems:
            return problems, None
        listed = len(copies)

    problems = []
    for source, copy in copies.items():
        try:
            want = get(commit, source)
        except NotFound as e:
            if listed is None:
                return [str(e)], None
            problems.append(
                f"{CANONICAL}'s {MANIFEST} at {commit[:12]}, the commit {CI} pins, lists {source}, "
                f"which is not in {CANONICAL} at that commit"
            )
            continue
        except FetchError as e:
            return [str(e)], None
        if not (root / copy).is_file():
            problems.append(
                f"{copy} is missing: it is {CANONICAL}'s {source} at {commit[:12]}, the commit {CI} pins; "
                "copy it unchanged from there"
            )
        elif (root / copy).read_bytes() != want:
            problems.append(
                f"{copy} is not {CANONICAL}'s {source} at {commit[:12]}, the commit {CI} pins: "
                "copy it unchanged from there"
            )

    try:
        names = tracked(root)
    except TrackedError as e:
        return [*problems, str(e)], None
    wanted = {copy.as_posix() for copy in copies.values()}
    for name in sorted(names):
        if is_copy_name(name) and name not in wanted:
            why = (
                f"{CANONICAL} at {commit[:12]}, the commit {CI} pins, has no {MANIFEST}, so it copies no skill or agent here"
                if listed is None
                else f"{CANONICAL}'s {MANIFEST} at {commit[:12]}, the commit {CI} pins, does not list it"
            )
            problems.append(
                f"{name} is a stale copy: {why}. Remove it; a skill or agent named twr-* comes only from there"
            )

    if not problems and (reason := config_problem(root / COPIES["agent/guard.py"], root / CONFIG)):
        problems.append(reason)
    return problems, listed


def check(root: Path = ROOT, get=fetch, tracked=tracked_files) -> list[str]:
    """What differs from the canonical files, one message each."""
    return _check(root, get, tracked)[0]


def main(root: Path = ROOT) -> int:
    problems, listed = _check(root, fetch, tracked_files)
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        return 1
    if listed is None:
        print(f"the guard and settings in .claude/ are {CANONICAL}'s agent/ at the commit {CI} pins")
    else:
        print(f"the {listed} copies in .claude/ are the files {CANONICAL}'s {MANIFEST} lists at the commit {CI} pins")
    return 0


if __name__ == "__main__":
    sys.exit(main())
