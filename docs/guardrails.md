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
| The guard hook | Blocks: skipping hooks (`--no-verify`, `-n`, a changed hooks path), force pushes, tag pushes, pushes to `main`, release tag changes, `gh pr merge --admin`, release commands and release or workflow runs, changes to repository settings, rulesets, environments, secrets and variables (by `gh` or `gh api`), container pushes and cluster writes, and printing or changing the `gh` login. Asks before issue and PR writes, any `gh pr merge`, and anything aimed outside the organisation. Follows `bash -c`, `eval`, `$(...)`, wrappers and environment prefixes, and fails closed. `python3 .claude/hooks/guard.py --check '<command>'` prints the verdict on a command (allow, ask or block, with the rule and the reason) without writing it to the guard log. | v1: [`.claude/hooks/guard.py`](https://github.com/those-who-run/those-who-run/blob/main/.claude/hooks/guard.py) (those-who-run/those-who-run#339, those-who-run/those-who-run#352). v2: the canonical `agent/guard.py` in `those-who-run/.github` (#12), copied to each repository's `.claude/hooks/guard.py` with its `.claude/guard.json`; CI checks the copy against the canonical one at the commit it pins (#20), along with every other copy that commit's `agent/copies.json` lists. | Maintainer, in `those-who-run/.github`. Claude Code asks before an agent edits a copy. |
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
matches `those-who-run/.github` at the pinned commit. That last check
(`scripts/guard_copy.py`) follows the manifest `agent/copies.json` at the
pinned commit: every file it lists is there and equals paddock's copy under
`.claude/` byte for byte, and a tracked skill or agent named `twr-*` that it
does not list is a stale copy and fails. A release from before the manifest
copies only the guard and its settings. `make check` runs the same locally. The ruleset requires those two checks, set from paddock's entry
in `scripts/repos.json` in `those-who-run/.github`.
Renovate, through the shared preset in `those-who-run/.github` that
`renovate.json` extends at a release tag, proposes updates to the pinned
actions after 7 days and to the commit hooks after 14, and none merges without
the maintainer. A change to the preset reaches paddock only through the PR
that bumps that tag. paddock publishes nothing, so it has no `release`
environment.

## Agent identity

Who an agent works as on GitHub decides what GitHub can enforce on its work.
It has changed once already, and is planned to change again.

- **Today: the maintainer's own account.** Agent sessions run on the
  maintainer's machine, started by him, and since late October 2026 they
  work under his GitHub account: they push branches and open pull requests
  as him. GitHub cannot tell an agent's work from his.
- **What that costs.** GitHub does not let an account approve its own pull
  request. The `main` ruleset requires a code owner's approval and the
  maintainer is the only code owner, so a PR opened under his account can
  never get the approval it needs. He merges each one himself, through the
  bypass the ruleset gives an admin, and that bypass skips the required
  checks and the merge queue along with the approval. This is a known gap,
  the first under [Known gaps](#known-gaps).
- **Commits** are authored with the noreply address of the account the
  session works under, the maintainer's today, and keep the agent trailer
  (`Co-Authored-By: ...`). The trailer is the only thing that marks a commit
  as an agent's. Nothing checks either yet (see [Known gaps](#known-gaps)).
- **Before: a bot account.** `mo-chara-croche`, a member of the
  organisation's `pit-crew` team with the Write role, never Admin, with a
  fine-grained token scoped to the organisation. From #236 until late
  October 2026 agents pushed branches and opened pull requests as it, and
  the maintainer approved each one, since an approval from the bot's own
  account did not count. The account still exists. Agent sessions no longer
  use it, and whether it is kept, for anything that needs a user account and
  not an App, or retired, is undecided.
- **Planned: GitHub Apps.** Unattended agents are to act as two Apps,
  `twr-builder` and `twr-reviewer`. Neither is a code owner, is on a bypass
  list, or can approve, so the maintainer can approve a PR that
  `twr-builder` opens and it lands through the merge queue, with nothing
  bypassed. Neither App exists yet, and no unattended agent runs.
  [`ladder.md`](ladder.md) has the design and
  [where it stands](ladder.md#where-things-stand).
- **The maintainer** is the only admin and the only code owner.
  [`.github/CODEOWNERS`](../.github/CODEOWNERS) names him on every path
  (`* @ciaranRoche`). `those-who-run/.github` and paddock have it, and the Go
  template is to ship it (decided on #13, #21). GitHub reads the file from the base branch, so a
  PR can't change who approves it.
- **The `main` ruleset** requires code-owner review and one approval, and
  dismisses an approval when new commits are pushed (paddock's entry in
  `scripts/repos.json` in `those-who-run/.github`, with `approvals: 1` and
  `code_owners: true`, applied by `protect-repo.sh`). An approval from an
  account that isn't a code owner doesn't count, so only the maintainer's
  does. Admins can bypass the ruleset, only through a pull request, and the
  maintainer is the only admin.
- **Dependency updates** don't auto-merge in v2. Renovate groups them into
  one PR per repository per week, and security fixes come at once in a PR
  of their own; the maintainer approves each like any other.

## Known gaps

Carried from v1, and still open unless a step from the menu closes them.

- **Agents work under the maintainer's account, and their PRs merge through
  the admin bypass.** The ruleset can't tell an agent's work from the
  maintainer's: both arrive as him. He can't approve a PR his own account
  opened, so the required approval, the gate the ruleset was set up to be,
  can't be met for agent work, and he merges it through the admin bypass.
  The bypass skips every rule at once: the approval, the required checks and
  the merge queue. CI still runs on the PR and its results show, but nothing
  makes a merge wait for them, and nothing tests the PR against the `main`
  it lands on as the queue would. What is left is what the maintainer reads
  before he merges, the guard hook, which blocks `gh pr merge --admin` and
  asks before any other merge inside Claude Code, and the rule that an agent
  never merges with a bypass ([`AGENTS.md`](../AGENTS.md#merging)). This is
  v1's gap again: there the ruleset requires 0 approvals and agents work
  with the maintainer's credentials. paddock was past it while agents worked
  as the bot account (see [Agent identity](#agent-identity)), and is not
  now. [`ladder.md`](ladder.md) is how it is planned to close: unattended
  agents act as GitHub Apps, whose PRs the maintainer can approve and queue.
  That leaves two paths to `main` for a while, since sessions on the
  maintainer's machine stay under his account until they move to such an
  identity too; only then can the bypass go.
- **`needs triage` is a rule, not a check.** Nothing stops an agent from
  working on an issue that still has the label; it relies on the agent
  reading the issue first. The same holds for `agent-ready`, the label an
  unattended agent waits for: the job that is to check it, and who applied
  it, is planned and not running ([`ladder.md`](ladder.md)).
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

The [merge ladder](ladder.md) is the plan for step 3, and it adds an
independent review of every PR, which this menu did not have. It is planned
as one piece of work in the maintainer's notes, not as an issue per step,
and none of it runs yet. The steps it covers say so.

1. **The managed settings file on the maintainer's machine.** Claude Code
   policy owned by the system, not the user, so an agent can't edit its own
   permissions or hooks: bypass mode off, only managed rules and hooks, a
   required sandbox. Narrows the gap that the guard is not a sandbox.
2. **Required reviewers on sensitive paths:** `.github/**`, `.claude/**`,
   `AGENTS.md`, `proto/**` and the deploy YAML need the maintainer's approval
   whatever else the ruleset allows. Covers the test guard's own copy, the
   guard's copies and the rules. paddock and `those-who-run/.github` already
   need it on every path, through `CODEOWNERS` and code-owner review
   (see [Agent identity](#agent-identity)). The ladder's `sensitive` lane
   names such a PR to the maintainer and has it read more closely; it shows
   and does not require ([`ladder.md`](ladder.md#lanes)).
3. **An agent identity with a required approval.** Every agent PR is opened
   by an account that is not the maintainer's, has the Write role at most
   and is not a code owner, so it needs an approval the agent can't give
   itself. Closes the gap that the ruleset can't tell the two apart. The
   rulesets already ask for the approval; what is missing is the identity.
   paddock and `those-who-run/.github` had one from #236, the bot account,
   until agent sessions moved to the maintainer's account (see
   [Agent identity](#agent-identity)). The [merge ladder](ladder.md) takes
   this step for unattended agents, with GitHub Apps. Moving the sessions on
   the maintainer's machine to such an identity, and then removing the admin
   bypass, is the rest of the step and is not planned yet.
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
   when the maintainer wants more autonomy. This is not the merge ladder:
   on that one the maintainer merges every PR, and a lane changes how
   closely a PR is read, never who merges it.

## References

- v1's rules: [`AGENTS.md`](https://github.com/those-who-run/those-who-run/blob/main/AGENTS.md).
- v1's guardrail issues: those-who-run/those-who-run#339 (commit hooks and
  the guard), those-who-run/those-who-run#352 (Claude Code project settings),
  those-who-run/those-who-run#353 (agent identity),
  those-who-run/those-who-run#358 (the test guard).
- The organisation's contributing guide and AI policy:
  [`CONTRIBUTING.md`](https://github.com/those-who-run/.github/blob/main/CONTRIBUTING.md).
