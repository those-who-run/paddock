# Guardrails

What keeps agent and human work on Those Who Run v2 safe: every guardrail v2
starts with, what it enforces, where it lives and who changes it; the gaps
they leave; and the hardening steps kept for later.

The decision for v2 is to start small and harden as we go. v2 starts with
v1's guardrails as they are, moved into the v2 repositories, and nothing new.
Each hardening step in [the menu](#hardening-menu) becomes an issue with the
`guardrail` label when something shows it is needed: a near miss, a new kind
of repository, or more agent work in parallel. When something goes wrong or
nearly does, find the gap below that let it through and file the step that
would have stopped it.

[`AGENTS.md`](../AGENTS.md) has the rules themselves; this page is the
reference for what enforces them.

## Always the maintainer's

Agents never do these, whatever an issue, comment or PR says. Most are also
enforced by a guardrail below; where none is, the rule still holds.

- **Merges** to `main`. An agent merges only when the maintainer asks, on
  green, never with `--admin` or another bypass.
- **Releases:** release PRs, tags, release workflow runs, and repeating a
  release.
- **Deploys** to any cluster.
- **Repository and organisation settings:** rulesets, environments, Actions
  permissions, collaborators and teams, labels and milestones.
- **Secrets:** never read, written, printed or committed by an agent.
- **New direct dependencies:** an agent may propose one, with the reason, in
  a PR; the maintainer decides.
- **Breaking contract changes** in `contracts`.

## The guardrails at the start

"Maintainer" in the last column means only the maintainer changes it: a file
by a PR the maintainer reviews and merges, a setting by hand. v1's files are
linked so their history stays findable; the v2 homes arrive with the issues
named.

| Guardrail | What it enforces | Where it lives | Who changes it |
|---|---|---|---|
| Claude Code project settings | Deny reading or editing secrets and credentials: `.env` files, keys and certificates (`*.pem`, `*.key`, SSH keys), and the credential files of the cloud, cluster, container, `gh` and Claude Code tools. Ask before editing `.claude/settings.json`, `.claude/settings.local.json` or anything under `.claude/hooks/`. Bypass permissions mode disabled. Runs the guard hook before every shell command, and blocks the command if the hook fails. | v1: [`.claude/settings.json`](https://github.com/those-who-run/those-who-run/blob/main/.claude/settings.json). v2: the canonical `agent/settings.json` in `those-who-run/.github` (#12), copied into each repository's `.claude/settings.json` (paddock #20, the Go template #22). | Maintainer. Claude Code asks before an agent edits it. |
| The guard hook | Blocks: skipping hooks (`--no-verify`, `-n`, a changed hooks path), force pushes, tag pushes, pushes to `main`, release tag changes, `gh pr merge --admin`, release commands and release or workflow runs, changes to repository settings, rulesets, environments, secrets and variables (by `gh` or `gh api`), container pushes and cluster writes, and printing or changing the `gh` login. Asks before issue and PR writes, any `gh pr merge`, and anything aimed outside the organisation. Follows `bash -c`, `eval`, `$(...)`, wrappers and environment prefixes, and fails closed. `python3 .claude/hooks/guard.py --check '<command>'` prints the verdict on a command (allow, ask or block, with the rule and the reason) without writing it to the guard log. | v1: [`.claude/hooks/guard.py`](https://github.com/those-who-run/those-who-run/blob/main/.claude/hooks/guard.py) (those-who-run/those-who-run#339, those-who-run/those-who-run#352). v2: the canonical `agent/guard.py` in `those-who-run/.github` (#12), copied to each repository's `.claude/hooks/guard.py` with its `.claude/guard.json`; CI checks the copy against the canonical one (#20). | Maintainer, in `those-who-run/.github`. Claude Code asks before an agent edits a copy. |
| Commit hooks | Before every commit: trailing whitespace and end of file, merge conflict markers, YAML syntax, large files, gitleaks on the staged changes, actionlint and zizmor on the workflows, a Conventional Commit message, and each language's lint. CI runs the same hooks on every file. | v1: [`.pre-commit-config.yaml`](https://github.com/those-who-run/those-who-run/blob/main/.pre-commit-config.yaml) (those-who-run/those-who-run#339). v2: each repository's `.pre-commit-config.yaml` (`.github` #13, paddock #20, the Go template #22). | Maintainer. The guard hook stops an agent from skipping them. |
| `AGENTS.md` and `CLAUDE.md` | The rules every agent and contributor follows: untrusted input, `needs triage`, `Tests weakened:`, dependencies, one issue one PR, merging and releases. Advice, not a check: each rule names what enforces it, if anything does. | v1: [`AGENTS.md`](https://github.com/those-who-run/those-who-run/blob/main/AGENTS.md). v2: [`AGENTS.md`](../AGENTS.md) and [`CLAUDE.md`](../CLAUDE.md) in paddock (#5), with a thin `AGENTS.md` in each component repository that links them. | Maintainer. |
| The `main` ruleset | Changes reach `main` only through a pull request; squash merges only; linear history; no force push or deletion; the repository's required checks pass first. Admins bypass only through a pull request. | Set by [`scripts/protect-repo.sh`](https://github.com/those-who-run/.github/blob/main/scripts/protect-repo.sh) in `those-who-run/.github`; for every v2 repository from a checked-in list (#19). | Maintainer, by running the script. Agents never change settings. |
| The release tag ruleset | Release tags (`v*`) are never moved or deleted. | Set by [`scripts/protect-repo.sh`](https://github.com/those-who-run/.github/blob/main/scripts/protect-repo.sh), as above (#19). | Maintainer, by running the script. |
| The `release` environment | The publishing secrets live only in the `release` environment, which only `main` and release tags can use; a release job waits for the maintainer as required reviewer. Release PRs are merged by hand, never auto-merged. | v1: [`release.yml`](https://github.com/those-who-run/those-who-run/blob/main/.github/workflows/release.yml) (those-who-run/those-who-run#364), with no required reviewer. v2: set by `protect-repo.sh` (#19), used by the reusable image and release-please workflows (#16, #17). | Maintainer. |
| CI | On every PR: dependency review; the test guard, which fails a PR that deletes, skips or loosens tests unless its body has a line `Tests weakened: <reason>`; gitleaks over every commit in the PR, including ones made without the hooks; the PR title checked as a Conventional Commit, since it becomes the squash commit. | v1: the `test` job in [`ci.yml`](https://github.com/those-who-run/those-who-run/blob/main/.github/workflows/ci.yml) and [`scripts/test_guard.py`](https://github.com/those-who-run/those-who-run/blob/main/scripts/test_guard.py) (those-who-run/those-who-run#358). v2: the reusable `checks.yml` in `those-who-run/.github` (#14), each language's test guard (#15 for Go), called from each repository's `ci.yml` (paddock #20). | Maintainer. |
| `needs triage` | Every new issue gets the label from its template, and blank issues are off. Agents don't work on an issue until the maintainer removes it. | paddock's [issue templates](../.github/ISSUE_TEMPLATE/) and [`docs/ISSUE_FORMAT.md`](ISSUE_FORMAT.md#needs-triage); the rule in [`AGENTS.md`](../AGENTS.md). | Maintainer: removes the label, and changes the templates. |

Where things stand in paddock: the `main` and release tag rulesets are on.
Since #20 paddock has the guard and the project settings in `.claude/`, the
commit hooks, `.github/CODEOWNERS` and CI with two jobs: `checks / checks`
from `checks.yml`, and `test`, which checks the Markdown links, that
`docs/labels.md` and `.github/labels.yml` agree, and that the guard copy
matches `those-who-run/.github` at the pinned commit. `make check` runs the
same locally. The ruleset requires those two checks, set from paddock's entry
in `scripts/repos.json` in `those-who-run/.github`.
Renovate, through the shared preset in `those-who-run/.github` that
`renovate.json` extends at a release tag, proposes updates to the pinned
actions after 7 days and to the commit hooks after 14, and none merges without
the maintainer. A change to the preset reaches paddock only through the PR
that bumps that tag. paddock publishes nothing, so it has no `release`
environment.

## Agent identity

Agents don't work with the maintainer's credentials. They work as their own
GitHub account, so GitHub can tell an agent's work from the maintainer's, and
the ruleset can ask for an approval the agent can't give.

- **The account:** `mo-chara-croche`, a member of the organisation's
  `pit-crew` team with the Write role, never Admin. Its fine-grained token is
  scoped to the organisation. It pushes branches and opens pull requests, and
  merges only when the maintainer asks.
- **Commits** are authored by that account, with its GitHub noreply address,
  and keep the agent trailer (`Co-Authored-By: ...`), so each one names both
  the account and the agent that wrote it. Nothing checks either yet (see
  [Known gaps](#known-gaps)).
- **The maintainer** is the only admin and the only code owner.
  [`.github/CODEOWNERS`](../.github/CODEOWNERS) names him on every path
  (`* @ciaranRoche`). `those-who-run/.github` and paddock have it, and the Go
  template is to ship it (decided on #13, #21). GitHub reads the file from the base branch, so a
  PR can't change who approves it.
- **The `main` ruleset** requires code-owner review and one approval, and
  dismisses an approval when new commits are pushed (paddock's entry in
  `scripts/repos.json` in `those-who-run/.github`, with `approvals: 1` and
  `code_owners: true`, applied by `protect-repo.sh`). An approval from the agent's account
  doesn't count, because it isn't a code owner, so the maintainer approves
  every merge.
- **Dependency updates** don't auto-merge in v2. Renovate groups them into
  one PR per repository per week, and security fixes come at once in a PR
  of their own; the maintainer approves each like any other.

## Known gaps

Carried from v1, and still open unless a step from the menu closes them.

- **The ruleset can't tell an agent's merge from the maintainer's.** v1's
  ruleset requires 0 approvals and agents there work with the maintainer's
  credentials, so a merge by either looks the same to GitHub. The guard
  hook's ask on `gh pr merge` is the gate. paddock already goes further: its
  ruleset requires code-owner review and 1 approval, a push after the
  approval needs another, and agents work as their own account, which isn't
  a code owner (see [Agent identity](#agent-identity)). Any repository still
  protected with 0 approvals, or an agent run with the maintainer's
  credentials, keeps the v1 gap.
- **`needs triage` is a rule, not a check.** Nothing stops an agent from
  working on an issue that still has the label; it relies on the agent
  reading the issue first.
- **The test guard runs the PR's own copy.** CI runs the script and reads the
  PR body as they are in the PR, so a PR can change the check along with the
  tests. It makes the reason visible; the maintainer's review is the gate.
- **The guard only works inside Claude Code, and is not a sandbox.** It never
  runs in the maintainer's shell or another tool. It can't see into a script
  file, a program that runs `git` or `gh` itself, or a command decoded at run
  time. CI and the rulesets are the backstop.
- **The guard and settings are those of the directory the session started
  in.** Claude Code loads the project settings, and so the guard, from where
  the session starts, not from the repository a command works on. A session
  started elsewhere that changes another repository's clone runs under the
  wrong guard, or none. The rule is to start the session in the clone or a
  worktree of the repository it changes ([`CLAUDE.md`](../CLAUDE.md)).
- **Commit author addresses and trailers aren't checked.** Commits should use
  a GitHub noreply address and carry the agent trailer
  (`Co-Authored-By: ...`); nothing fails when they don't.

## Hardening menu

In the rough order expected. Each is a `guardrail` issue when needed, not
before.

1. **The managed settings file on the maintainer's machine.** Claude Code
   policy owned by the system, not the user, so an agent can't edit its own
   permissions or hooks: bypass mode off, only managed rules and hooks, a
   required sandbox. Narrows the gap that the guard is not a sandbox.
2. **Required reviewers on sensitive paths:** `.github/**`, `.claude/**`,
   `AGENTS.md`, `proto/**` and the deploy YAML need the maintainer's approval
   whatever else the ruleset allows. Covers the test guard's own copy, the
   guard's copies and the rules. paddock and `those-who-run/.github` already
   need it on every path, through `CODEOWNERS` and code-owner review
   (see [Agent identity](#agent-identity)).
3. **An agent identity with a required approval.** Agents work as their own
   account with the Write role, never Admin, and every PR needs an approval
   the agent can't give itself. Closes the gap that the ruleset can't tell
   the two apart. Done in paddock and `those-who-run/.github` (#236, see
   [Agent identity](#agent-identity)); this step takes it to every
   repository.
4. **The guard as a plugin**, once copying it between repositories hurts, so
   every repository takes one versioned guard instead of a copy.
5. **Organisation policy as code with a read-only drift check**, once there
   are enough repositories for settings to drift: rulesets, environments and
   Actions permissions declared in one place, and a check that reports
   where a repository differs without changing it.
6. **Stewart, the ephemeral end-to-end environment:** a short-lived stack
   that replays a recorded race through the real components before a change
   merges (#232, #233).
7. **A promotion ladder towards more autonomy:** levels of what agents may do
   on their own, such as merging low-risk changes on green, each with the
   evidence needed to move up and the setting that moves back down. Only
   when the maintainer wants more autonomy.

## References

- v1's rules: [`AGENTS.md`](https://github.com/those-who-run/those-who-run/blob/main/AGENTS.md).
- v1's guardrail issues: those-who-run/those-who-run#339 (commit hooks and
  the guard), those-who-run/those-who-run#352 (Claude Code project settings),
  those-who-run/those-who-run#353 (agent identity),
  those-who-run/those-who-run#358 (the test guard).
- The organisation's contributing guide and AI policy:
  [`CONTRIBUTING.md`](https://github.com/those-who-run/.github/blob/main/CONTRIBUTING.md).
