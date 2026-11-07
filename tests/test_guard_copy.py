"""The guard copy check (scripts/guard_copy.py), offline: the canonical files, and the
manifest that lists them, come from a stand-in for the fetch. `make guard` runs the
real check against GitHub."""

from __future__ import annotations

import importlib.util
import io
import json
import os
import shutil
import subprocess
import tempfile
import unittest
import urllib.error
from contextlib import redirect_stderr, redirect_stdout
from unittest import mock
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

_spec = importlib.util.spec_from_file_location("guard_copy", ROOT / "scripts" / "guard_copy.py")
assert _spec is not None and _spec.loader is not None
guard_copy = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(guard_copy)

SHA = "a" * 40
CI = f"""jobs:
  checks:
    uses: those-who-run/.github/.github/workflows/checks.yml@{SHA} # v0.2.0
"""


# The copies a release with skills lists (the copy contract): the two guard files, a
# skill and the reviewer's brief.
SKILL = ".claude/skills/twr-dev/SKILL.md"
BRIEF = ".claude/agents/twr-reviewer.md"
LISTED = {
    "agent/agents/twr-reviewer.md": BRIEF,
    "agent/guard.py": ".claude/hooks/guard.py",
    "agent/settings.json": ".claude/settings.json",
    "agent/skills/twr-dev/SKILL.md": SKILL,
}


def git(root: Path, *args: str) -> subprocess.CompletedProcess:
    """git in `root`, whatever repository a hook running the tests has in its environment."""
    env = {k: v for k, v in os.environ.items() if k not in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE")}
    return subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True, env=env)


class Pin(unittest.TestCase):
    def test_this_repositorys_ci_pins_one_commit(self):
        sha = guard_copy.pinned_commit((ROOT / guard_copy.CI).read_text())
        self.assertRegex(sha, r"^[0-9a-f]{40}$")

    def test_a_tag_is_not_a_pin(self):
        with self.assertRaises(ValueError):
            guard_copy.pinned_commit(CI.replace(SHA, "v0.2.0"))

    def test_two_different_pins_are_refused(self):
        with self.assertRaises(ValueError):
            guard_copy.pinned_commit(CI + CI.replace(SHA, "b" * 40).split("\n", 1)[1])


class Repository(unittest.TestCase):
    """A repository with paddock's .claude/ and a ci.yml pinned at SHA."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)
        shutil.copytree(ROOT / ".claude", self.root / ".claude",
                        ignore=shutil.ignore_patterns("worktrees", "settings.local.json"))
        (self.root / guard_copy.CI).parent.mkdir(parents=True)
        (self.root / guard_copy.CI).write_text(CI)
        # A repository of its own, so the check for stale copies asks git about this
        # tree and not about paddock's.
        self.assertEqual(git(self.root, "init", "-q").returncode, 0)
        self.track()
        self.asked: list[tuple[str, str]] = []
        # What .github holds at the pin: paddock's own guard and settings, and no
        # manifest, as every release before the skills.
        self.github: dict[str, bytes] = {
            source: (ROOT / copy).read_bytes() for source, copy in guard_copy.COPIES.items()
        }

    def track(self):
        """Stage everything, which is what makes a file tracked."""
        added = git(self.root, "add", "--all")
        self.assertEqual(added.returncode, 0, added.stderr)

    def write(self, path: str, content: bytes = b"# a copy\n"):
        (self.root / path).parent.mkdir(parents=True, exist_ok=True)
        (self.root / path).write_bytes(content)

    def release_with_skills(self, manifest: object = None):
        """Move the pin's .github to one with a manifest, a skill and the reviewer's
        brief, and copy them here as the fan-out would."""
        self.github["agent/skills/twr-dev/SKILL.md"] = b"# the development loop\n"
        self.github["agent/agents/twr-reviewer.md"] = b"# the reviewer\n"
        self.github[guard_copy.MANIFEST] = (
            manifest if isinstance(manifest, bytes)
            else json.dumps(LISTED if manifest is None else manifest, indent=2, sort_keys=True).encode()
        )
        self.write(SKILL, self.github["agent/skills/twr-dev/SKILL.md"])
        self.write(BRIEF, self.github["agent/agents/twr-reviewer.md"])
        self.track()

    def canonical(self, commit: str, path: str) -> bytes:
        """What .github holds at `commit`, and a 404 for what it does not."""
        self.asked.append((commit, path))
        if path not in self.github:
            raise guard_copy.NotFound(f"could not fetch {path} (Not Found)")
        return self.github[path]

    def check(self) -> list[str]:
        return guard_copy.check(self.root, self.canonical)


class Copies(Repository):
    """A pin from before the manifest: the guard and the settings, as it always was."""

    def test_the_same_files_pass_and_are_fetched_at_the_pin(self):
        self.assertEqual(guard_copy.check(self.root, self.canonical), [])
        self.assertEqual(
            sorted(self.asked),
            [(SHA, "agent/copies.json"), (SHA, "agent/guard.py"), (SHA, "agent/settings.json")],
        )

    def test_a_changed_guard_fails(self):
        guard = self.root / ".claude/hooks/guard.py"
        guard.write_bytes(guard.read_bytes() + b"\n# changed\n")
        problems = guard_copy.check(self.root, self.canonical)
        self.assertEqual(len(problems), 1)
        self.assertIn(".claude/hooks/guard.py is not those-who-run/.github's agent/guard.py", problems[0])

    def test_a_missing_settings_file_fails(self):
        (self.root / ".claude/settings.json").unlink()
        problems = guard_copy.check(self.root, self.canonical)
        self.assertEqual(len(problems), 1)
        self.assertIn(".claude/settings.json", problems[0])

    def test_a_config_the_guard_cannot_read_fails(self):
        (self.root / ".claude/guard.json").write_text('{"branch": "main", "brnach": "main"}\n')
        problems = guard_copy.check(self.root, self.canonical)
        self.assertEqual(len(problems), 1)
        self.assertIn("the guard can't read it", problems[0])

    def test_no_pin_fails_without_fetching(self):
        (self.root / guard_copy.CI).write_text("jobs: {}\n")
        self.assertEqual(len(guard_copy.check(self.root, self.canonical)), 1)
        self.assertEqual(self.asked, [])

    def test_a_failed_fetch_is_one_line_not_a_traceback(self):
        def refused(*_args, **_kwargs):
            raise urllib.error.HTTPError("https://example.invalid", 404, "Not Found", {}, io.BytesIO())

        with mock.patch.object(guard_copy.urllib.request, "urlopen", refused):
            with self.assertRaises(guard_copy.FetchError) as raised:
                guard_copy.fetch(SHA, "agent/guard.py")
            message = str(raised.exception)
            self.assertIn(f"raw.githubusercontent.com/those-who-run/.github/{SHA}/agent/guard.py", message)
            self.assertNotIn("\n", message)

            with mock.patch.object(guard_copy, "fetch", side_effect=guard_copy.FetchError(message)):
                stderr = io.StringIO()
                with redirect_stderr(stderr):
                    self.assertEqual(guard_copy.main(self.root), 1)
            self.assertEqual(stderr.getvalue(), message + "\n")

    def test_only_a_404_is_not_found(self):
        # The check reads NotFound, for the manifest, as a release from before it; a
        # server error or a rate limit must not pass for that.
        def answer(code: int):
            def refused(*_args, **_kwargs):
                raise urllib.error.HTTPError("https://example.invalid", code, "refused", {}, io.BytesIO())
            return refused

        with mock.patch.object(guard_copy.urllib.request, "urlopen", answer(404)):
            with self.assertRaises(guard_copy.NotFound):
                guard_copy.fetch(SHA, guard_copy.MANIFEST)
        for code in (403, 429, 500, 503):
            with mock.patch.object(guard_copy.urllib.request, "urlopen", answer(code)):
                with self.assertRaises(guard_copy.FetchError) as raised:
                    guard_copy.fetch(SHA, guard_copy.MANIFEST)
            self.assertNotIsInstance(raised.exception, guard_copy.NotFound, code)

    def test_no_network_is_not_not_found(self):
        def offline(*_args, **_kwargs):
            raise urllib.error.URLError(OSError("Name or service not known"))

        with mock.patch.object(guard_copy.urllib.request, "urlopen", offline):
            with self.assertRaises(guard_copy.FetchError) as raised:
                guard_copy.fetch(SHA, guard_copy.MANIFEST)
        self.assertNotIsInstance(raised.exception, guard_copy.NotFound)

    def test_a_path_cannot_cut_the_url_short(self):
        asked = []

        def served(url, **_kwargs):
            asked.append(url)
            raise urllib.error.HTTPError(url, 404, "Not Found", {}, io.BytesIO())

        with mock.patch.object(guard_copy.urllib.request, "urlopen", served):
            with self.assertRaises(guard_copy.NotFound):
                guard_copy.fetch(SHA, "agent/guard.py?x#y")
        self.assertEqual(
            asked, [f"https://raw.githubusercontent.com/those-who-run/.github/{SHA}/agent/guard.py%3Fx%23y"]
        )

    def test_no_network_is_one_line_too(self):
        def offline(*_args, **_kwargs):
            raise urllib.error.URLError(OSError("Name or service not known"))

        with mock.patch.object(guard_copy.urllib.request, "urlopen", offline):
            with self.assertRaises(guard_copy.FetchError) as raised:
                guard_copy.fetch(SHA, "agent/settings.json")
        self.assertIn("Name or service not known", str(raised.exception))


class Manifest(Repository):
    """A pin whose .github has agent/copies.json: what it lists is what is checked."""

    def test_every_listed_copy_passes_and_is_fetched_at_the_pin(self):
        self.release_with_skills()
        self.assertEqual(self.check(), [])
        # The manifest first, then what it lists, all at the pin.
        self.assertEqual(self.asked, [(SHA, guard_copy.MANIFEST)] + [(SHA, s) for s in sorted(LISTED)])

    def test_a_changed_skill_fails(self):
        self.release_with_skills()
        self.write(SKILL, b"# the development loop, edited here\n")
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(f"{SKILL} is not those-who-run/.github's agent/skills/twr-dev/SKILL.md", problems[0])

    def test_a_copy_that_differs_by_one_trailing_byte_fails(self):
        self.release_with_skills()
        self.write(BRIEF, self.github["agent/agents/twr-reviewer.md"] + b"\n")
        self.assertEqual(len(self.check()), 1)

    def test_a_missing_destination_fails_and_is_named(self):
        self.release_with_skills()
        (self.root / SKILL).unlink()
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(f"{SKILL} is missing", problems[0])
        self.assertIn("agent/skills/twr-dev/SKILL.md", problems[0])

    def test_a_directory_at_a_destination_is_not_a_file_not_a_crash(self):
        self.release_with_skills()
        (self.root / BRIEF).unlink()
        (self.root / BRIEF).mkdir()
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(f"{BRIEF} is not a file", problems[0])

    def test_a_source_outside_agent_is_checked_like_any_other(self):
        # As chassis's copy of this check does: the manifest says where a copy comes
        # from, and the destination is what is held to .claude/.
        listed = {**LISTED, "docs/notes.md": ".claude/skills/twr-dev/notes.md"}
        self.release_with_skills(listed)
        self.github["docs/notes.md"] = b"# notes\n"
        self.write(".claude/skills/twr-dev/notes.md", b"# notes\n")
        self.track()
        self.assertEqual(self.check(), [])
        self.write(".claude/skills/twr-dev/notes.md", b"# other notes\n")
        self.assertEqual(len(self.check()), 1)

    def test_a_listed_source_that_is_not_at_the_pin_fails_and_is_named(self):
        self.release_with_skills()
        del self.github["agent/skills/twr-dev/SKILL.md"]
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn("lists agent/skills/twr-dev/SKILL.md, which is not in those-who-run/.github", problems[0])

    def test_every_wrong_copy_is_reported_not_only_the_first(self):
        self.release_with_skills()
        (self.root / SKILL).unlink()
        self.write(BRIEF, b"# another reviewer\n")
        self.assertEqual(len(self.check()), 2)

    def test_the_guard_config_is_still_read(self):
        self.release_with_skills()
        (self.root / ".claude/guard.json").write_text('{"branch": "main", "brnach": "main"}\n')
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn("the guard can't read it", problems[0])

    def test_a_manifest_that_cannot_be_fetched_is_a_failure_not_no_manifest(self):
        # Only a 404 means "no manifest". Anything else (the network, a 500) must not
        # fall back to checking the two guard files and passing.
        def unreachable(commit: str, path: str) -> bytes:
            if path == guard_copy.MANIFEST:
                raise guard_copy.FetchError("could not fetch agent/copies.json (timed out)")
            return self.canonical(commit, path)

        self.assertEqual(
            guard_copy.check(self.root, unreachable), ["could not fetch agent/copies.json (timed out)"]
        )
        self.assertEqual(self.asked, [])

    def test_main_says_how_many_copies_the_manifest_lists(self):
        self.release_with_skills()
        out, err = io.StringIO(), io.StringIO()
        with mock.patch.object(guard_copy, "fetch", self.canonical):
            with redirect_stdout(out), redirect_stderr(err):
                self.assertEqual(guard_copy.main(self.root), 0, err.getvalue())
        self.assertIn("the 4 copies in .claude/", out.getvalue())
        self.assertIn("agent/copies.json", out.getvalue())

    def test_main_without_a_manifest_says_what_it_always_said(self):
        out = io.StringIO()
        with mock.patch.object(guard_copy, "fetch", self.canonical), redirect_stdout(out):
            self.assertEqual(guard_copy.main(self.root), 0)
        self.assertEqual(
            out.getvalue(),
            "the guard and settings in .claude/ are those-who-run/.github's agent/ "
            "at the commit .github/workflows/ci.yml pins\n",
        )


class MalformedManifest(Repository):
    """A manifest the check can't trust is a failure: never a crash, never a pass."""

    def refused(self, manifest: object, said: str):
        """The manifest fails the check with one message holding `said`, and nothing
        it lists is fetched."""
        self.release_with_skills(manifest)
        problems = self.check()
        self.assertEqual(len(problems), 1, problems)
        self.assertIn("agent/copies.json", problems[0])
        self.assertIn(said, problems[0])
        self.assertEqual(self.asked, [(SHA, guard_copy.MANIFEST)])

    def test_not_json(self):
        self.refused(b"{not json", "can't be read")

    def test_empty(self):
        self.refused(b"", "can't be read")

    def test_not_utf8(self):
        self.refused(b"\xff\xfe{}", "can't be read")

    def test_a_list_not_an_object(self):
        self.refused(sorted(LISTED), "is not a JSON object")

    def test_a_source_listed_twice(self):
        twice = b'{"agent/guard.py": ".claude/hooks/guard.py", "agent/guard.py": ".claude/hooks/other.py"}'
        self.refused(twice, "lists agent/guard.py twice")

    def test_a_destination_that_is_not_a_string(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": [SKILL]}, "is not a path")

    def test_a_destination_outside_dot_claude(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": "docs/SKILL.md"}, "is not under .claude/")

    def test_a_destination_that_is_dot_claude_itself(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": ".claude"}, "is not under .claude/")

    def test_a_destination_that_only_starts_like_dot_claude(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": ".claude-old/x.md"}, "is not under .claude/")

    def test_an_absolute_destination(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": "/.claude/x.md"}, "not a plain relative path")

    def test_a_destination_with_dot_dot(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": ".claude/../AGENTS.md"}, "has `..` in it")

    def test_a_destination_with_a_backslash(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": ".claude\\skills\\x.md"}, "not a plain relative path")

    def test_a_destination_with_an_empty_part(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": ".claude//x.md"}, "empty or `.` part")

    def test_a_source_with_dot_dot(self):
        self.refused({**LISTED, "agent/../scripts/fan_out.py": ".claude/skills/twr-x/SKILL.md"}, "has `..` in it")

    def test_an_absolute_source(self):
        self.refused({**LISTED, "/agent/skills/twr-x/SKILL.md": ".claude/skills/twr-x/SKILL.md"}, "not a plain relative path")

    def test_a_path_with_a_control_character(self):
        self.refused({**LISTED, "agent/skills/twr-dev/SKILL.md": ".claude/skills/twr-dev/SKILL\n.md"}, "not a plain relative path")

    def test_two_sources_for_one_destination(self):
        self.refused({**LISTED, "agent/skills/twr-proof/SKILL.md": SKILL}, f"to {SKILL}")

    def test_a_manifest_without_the_guard(self):
        without = {k: v for k, v in LISTED.items() if k != "agent/guard.py"}
        self.refused(without, "does not send agent/guard.py to .claude/hooks/guard.py")

    def test_a_manifest_that_sends_the_guard_elsewhere(self):
        self.refused({**LISTED, "agent/guard.py": ".claude/hooks/other.py"}, "does not send agent/guard.py")

    def test_an_empty_object_names_both_guard_files(self):
        self.release_with_skills({})
        problems = self.check()
        self.assertEqual(len(problems), 2, problems)
        self.assertIn("agent/guard.py", problems[0])
        self.assertIn("agent/settings.json", problems[1])

    def test_every_bad_entry_is_reported(self):
        self.release_with_skills({**LISTED, "agent/a": "../a", "agent/b": "/b"})
        self.assertEqual(len(self.check()), 2)


class Stale(Repository):
    """A tracked skill or agent named twr-* that the pin's .github does not send here."""

    def test_a_skill_with_no_manifest_is_stale(self):
        self.write(SKILL)
        self.track()
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(f"{SKILL} is a stale copy", problems[0])
        self.assertIn("has no agent/copies.json", problems[0])

    def test_an_agent_with_no_manifest_is_stale(self):
        self.write(BRIEF)
        self.track()
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(f"{BRIEF} is a stale copy", problems[0])

    def test_a_skill_the_manifest_dropped_is_stale(self):
        self.release_with_skills()
        self.write(".claude/skills/twr-old/SKILL.md")
        self.write(".claude/skills/twr-old/notes/more.md")
        self.track()
        problems = self.check()
        self.assertEqual(len(problems), 2, problems)
        self.assertIn(".claude/skills/twr-old/SKILL.md is a stale copy", problems[0])
        self.assertIn(".claude/skills/twr-old/notes/more.md is a stale copy", problems[1])
        self.assertIn("does not list it", problems[0])

    def test_a_second_file_in_a_listed_skill_is_stale(self):
        self.release_with_skills()
        self.write(".claude/skills/twr-dev/extra.md")
        self.track()
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(".claude/skills/twr-dev/extra.md is a stale copy", problems[0])

    def test_an_agent_the_manifest_dropped_is_stale(self):
        self.release_with_skills()
        self.write(".claude/agents/twr-old.md")
        self.track()
        problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(".claude/agents/twr-old.md is a stale copy", problems[0])

    def test_a_file_that_is_not_tracked_is_left_alone(self):
        self.release_with_skills()
        self.write(".claude/skills/twr-draft/SKILL.md")  # written, never added
        self.assertEqual(self.check(), [])

    def test_other_names_are_not_copies(self):
        self.release_with_skills()
        for path in (
            ".claude/skills/mine/SKILL.md",
            ".claude/skills/mine/twr-notes/x.md",
            ".claude/skills/twr-loose.md",
            ".claude/agents/mine.md",
            ".claude/agents/twr-notes.txt",
            ".claude/agents/twr-folder/brief.md",
            "docs/.claude/skills/twr-dev/SKILL.md",
        ):
            self.write(path)
        self.track()
        self.assertEqual(self.check(), [])

    def test_stale_and_wrong_copies_are_reported_together(self):
        self.release_with_skills()
        self.write(SKILL, b"# edited here\n")
        self.write(".claude/agents/twr-old.md")
        self.track()
        self.assertEqual(len(self.check()), 2)

    def test_outside_a_repository_the_files_on_disk_are_checked(self):
        # Where git can't say what is tracked, the check must not pass for want of
        # an answer: every file on disk counts.
        self.release_with_skills()
        self.write(".claude/skills/twr-old/SKILL.md")
        shutil.rmtree(self.root / ".git")
        # Above the temporary directory there may be a repository; stop git looking.
        with mock.patch.dict(os.environ, {"GIT_CEILING_DIRECTORIES": str(self.root.parent)}):
            problems = self.check()
            self.assertEqual(len(problems), 1, problems)
            self.assertIn(".claude/skills/twr-old/SKILL.md is a stale copy", problems[0])
            shutil.rmtree(self.root / ".claude/skills/twr-old")
            self.assertEqual(self.check(), [])

    def test_without_git_the_files_on_disk_are_checked(self):
        self.write(BRIEF)
        with mock.patch.object(guard_copy.subprocess, "run", side_effect=FileNotFoundError("git")):
            problems = self.check()
        self.assertEqual(len(problems), 1)
        self.assertIn(f"{BRIEF} is a stale copy", problems[0])


class Ignored(unittest.TestCase):
    """.gitignore keeps .claude/ out but for the copies and paddock's config, so a
    copy the fan-out writes can be committed and a worktree can't."""

    def ignored(self, path: str) -> bool:
        # --no-index: what the patterns say, whether or not the path is tracked yet.
        answer = git(ROOT, "check-ignore", "-q", "--no-index", path)
        self.assertIn(answer.returncode, (0, 1), answer.stderr)
        return answer.returncode == 0

    def test_the_copies_can_be_tracked(self):
        for path in (*LISTED.values(), ".claude/guard.json", ".claude/skills/twr-dev/notes/more.md"):
            self.assertFalse(self.ignored(path), path)

    def test_the_rest_of_dot_claude_stays_out(self):
        for path in (
            ".claude/worktrees/i1/README.md",
            ".claude/worktrees/i1/.claude/skills/twr-dev/SKILL.md",
            ".claude/settings.local.json",
            ".claude/hooks/other.py",
            ".claude/hooks/__pycache__/guard.cpython-314.pyc",
        ):
            self.assertTrue(self.ignored(path), path)


class PaddocksConfig(unittest.TestCase):
    """paddock's own values in .claude/guard.json."""

    def test_the_guard_config_values(self):
        config = json.loads((ROOT / guard_copy.CONFIG).read_text(encoding="utf-8"))
        self.assertEqual(
            config,
            {
                "branch": "main",
                "release_tags": ["v*"],
                "release_workflow": "release.yml",
                "owner": "those-who-run",
            },
        )


if __name__ == "__main__":
    unittest.main()
