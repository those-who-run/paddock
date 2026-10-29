@AGENTS.md

## Claude Code

The rules are in `AGENTS.md` above; these notes are only for Claude Code.

- Work in a git worktree per issue, on a branch from `origin/main`. Worktrees
  live in `.claude/worktrees/` and are never committed (`.gitignore` keeps
  them out). Each new worktree sets the noreply address of the account it
  works as, never the maintainer's, unless the environment already sets it,
  with `git config user.email <id>+<user>@users.noreply.github.com` before
  the first commit, then runs the repository's setup target from its own
  `AGENTS.md`.
- Start the session for a repository in that repository's clone or one of
  its worktrees, not in another directory: Claude Code loads the project
  settings and the guard of the directory the session starts in. A
  coordinator handing work to an agent in another repository starts a
  session there for it.
- Commits end with the trailer Claude Code writes,
  `Co-Authored-By: Claude <model> <noreply@anthropic.com>`, naming the model
  you are.
- No session links in commits or PR bodies.
- `gh pr create --body` replaces the organisation's
  [PR template](https://github.com/those-who-run/.github/blob/main/PULL_REQUEST_TEMPLATE.md)
  rather than filling it, so write its parts yourself: "What changed" with
  its "AI assistance" and "New direct dependencies" lines, "How it was
  checked" with its two checkboxes, a `Tests weakened:` line if one applies,
  then the `Closes` or `Part of` line. The line Claude Code adds,
  `🤖 Generated with [Claude Code](https://claude.com/claude-code)`, goes
  last, after it.
- Use `gh` for issues and PRs. Read an issue with
  `gh issue view N -R those-who-run/paddock --comments` before anything else;
  check its labels for `needs triage` and `hold`, and its author. Open the PR
  in the component's repository with `gh pr create`, ready for review.
- The Claude Code project settings and the guard hook deny the
  outward-facing commands (merging with `--admin`, pushing tags, running
  release workflows, changing settings), ask before `gh pr merge`, and keep
  agents away from secrets. When the guard blocks a command, don't work
  around it: stop and say what was blocked. Where the settings or the guard
  are not in your checkout, `AGENTS.md` is the only guard, so follow it as if
  they were there. [`docs/guardrails.md`](docs/guardrails.md) has the detail.
- In a component repository, Claude Code loads that repository's own
  `CLAUDE.md`, which links back here. Read this file and `AGENTS.md` from
  `paddock` as well; the shared rules apply in every repository.
