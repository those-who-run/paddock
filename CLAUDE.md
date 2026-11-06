@AGENTS.md

## Claude Code

The rules are in `AGENTS.md` above; these notes are only for Claude Code.

- Work in a git worktree per issue, on a branch from `origin/main`. Worktrees
  live in `.claude/worktrees/` and are never committed (`.gitignore` keeps
  them out). Each new worktree commits with the noreply address of the
  account the session works under, never another account's. In a session on
  the maintainer's machine that account is the maintainer's, and git there
  already has his address, so there is nothing to set; where git has no
  address or the wrong one, set it with
  `git config user.email <id>+<user>@users.noreply.github.com` before the
  first commit. Then run the repository's setup target from its own
  `AGENTS.md`.
- Once a repository has the skills in `.claude/skills/`, they are the
  procedure to follow. Start any change with `twr-dev`, the loop for one
  issue and one pull request; it chains `twr-proof` (the evidence each
  acceptance criterion needs), `twr-test-audit` (before a test is added or
  changed), `twr-deslop` (a clean-up of the diff that changes no behaviour)
  and `twr-pr-ready` (the readiness checklist and the PR body). Answer a
  review with `twr-fix-review`. They are copies of `agent/skills/` in
  `those-who-run/.github`, never edited in a repository, and they are the
  order to apply the rules in, not rules of their own: where a skill and
  `AGENTS.md` differ, `AGENTS.md` wins. A repository without them follows
  `AGENTS.md` and these notes directly;
  [`docs/ladder.md`](docs/ladder.md#where-things-stand) says where the
  skills stand.
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
  checked" with the evidence table and then its two checkboxes, a
  `Tests weakened:` line if one applies, then the `Closes` or `Part of`
  line. The line Claude Code adds,
  `🤖 Generated with [Claude Code](https://claude.com/claude-code)`, goes
  last, after it.
- The evidence table has one row per acceptance criterion, in the issue's
  order, and three columns: Criterion, Proof, Where. Proof is the kind of
  evidence, one of `unit`, `recording`, `parity`, `e2e`, `live` or `car`.
  Where names the test or the command, and the recording when there is one.
  An agent never claims `live` (the maintainer's cluster) or `car` (his car,
  phone or tablet): only the maintainer produces them. A criterion that
  needs one keeps its row, which says it was not run here and who checks
  it, and the PR says `Part of`, not `Closes`. A row is never left out, and
  the proof named is the one that was run, nothing stronger.
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
