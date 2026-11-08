# The merge ladder

How a change gets from an issue to `main` when agents write the code: the
steps every pull request climbs, who takes each one, and what enforces it or
shows it. Claude agents write the code and open the pull requests. Each PR is
built with shared skills, checked by CI, read by an independent reviewer
agent, and merged last by the maintainer. The maintainer is the last rung on
every PR, and nothing on this page changes that.

**Most of this is still a design, not a description.** Of the unattended
jobs, only the reviewer runs. [Where things stand](#where-things-stand) says
which parts exist; read it before relying on anything below.

[`AGENTS.md`](../AGENTS.md) has the rules an agent follows today, and
[`guardrails.md`](guardrails.md) what enforces them. This page is the
reference for where they are heading.

## Where things stand

Rung 3 runs; the build, fix and ready rungs do not. Since 2026-11-07 the
review job runs unattended on the maintainer's cluster as the `twr-reviewer`
App, and it has posted reviews: one marker comment per head commit and the
status `review / verdict`. No unattended agent builds or fixes anything yet,
and nothing sets `ladder / ready` or `ladder / stuck`.

What exists:

| Piece | Where |
|---|---|
| The rules agents and people follow | [`AGENTS.md`](../AGENTS.md), [`CLAUDE.md`](../CLAUDE.md) |
| The guard hook and the Claude Code project settings | Each repository's `.claude/`, copied from `those-who-run/.github` ([`guardrails.md`](guardrails.md#the-guardrails-at-the-start)) |
| The skills, the reviewer's brief and the lanes | `those-who-run/.github`'s `agent/`; each repository's `.claude/skills/` once its pinned release carries them |
| The `main` ruleset: a pull request, one approval by a code owner, a new approval after a push, squash merges, the required checks | Every repository, set from `those-who-run/.github` |
| The merge queue | Every repository but `those-who-run/.github`, which merges without one |
| CI's required checks | Each repository's workflows |
| `twr-release`, the GitHub App that release-please and the release fan-out run as | The organisation |
| `twr-reviewer`, the GitHub App the review job posts as | The organisation; the job runs on the maintainer's cluster, from `those-who-run/pit-crew` |
| The review job and the status `review / verdict` | Running since 2026-11-07 |
| The labels `agent-ready`, `hold` and `re-review` | Described in [`labels.md`](labels.md#process); the maintainer applies them to GitHub by hand |

What is being built, and not running yet:

| Piece | State |
|---|---|
| The build and fix jobs, and the `twr-builder` App they publish as | Being built in `those-who-run/pit-crew`. Nothing reads `agent-ready` yet, so the label starts nothing. |
| The step that marks a PR ready, and the statuses `ladder / ready` and `ladder / stuck` | Planned. Nothing sets them. |
| The evidence table in pull request bodies | Described by the skills and by [`CLAUDE.md`](../CLAUDE.md). The reviewer reads it; no check enforces it. |
| `re-review` being acted on | Proposed in [those-who-run/pit-crew#8](https://github.com/those-who-run/pit-crew/pull/8), which shows whether it has merged. Until it does, a held review is read again only at the PR's next head. |

A held review: when the publish step refuses the reviewer's output (a
private value or a mention in it, or no verdict written at all), it posts no
comment, sets `review / verdict` to `error` with `held:` and the reason, and
keeps the output for the maintainer. The status counts as the review at that
head, so the job does not read the PR again until its head moves. The
`re-review` label is how the maintainer is to ask for another read of the
same head: it counts only when he applied it after the held status, and the
publish step removes it when it posts. That waits on the pit-crew change in
the table above. Like every verdict, a held
one is advice: it is not a required check, and it neither blocks nor allows a
merge ([why](#why-the-verdict-is-advisory)).

How agent work reaches `main` today: agent sessions run on the maintainer's
machine, started by him, under his own GitHub account. He cannot approve a PR
his own account opened, so he merges each one himself through the bypass the
`main` ruleset gives an admin, which skips the required approval, the
required checks and the merge queue. That is a known gap
([`guardrails.md`](guardrails.md#known-gaps)), and the build job, opening
PRs as `twr-builder`, is how it is planned to close. The reviewer reads
those PRs too; its verdict does not change how they merge.

## The rungs

Every pull request climbs the same rungs, in this order.

| Rung | Gate | Who | Enforced or shown by |
|---|---|---|---|
| 0 Spec | The issue is triaged, then labelled `agent-ready` | The maintainer | The job that picks up work checks that the maintainer applied the label, and that `needs triage` and `hold` are absent |
| 1 Build | The development loop: implement, choose the proof, audit the tests, clean the diff, check it, write the PR body | `twr-builder`, in a build job | The skills from `those-who-run/.github`'s `agent/`; a publish step with no model in it, which refuses bad output |
| 2 CI | The repository's required checks | GitHub Actions | The `main` ruleset |
| 3 Review | An independent read at the PR's head commit, acceptance criteria first | `twr-reviewer`, in a review job | One marker comment per head commit, and a commit status `review / verdict` |
| 3b Fix | The builder answers each item the review asked for, and the reviewer reads again | The builder and the reviewer | Two rounds at most, counted from markers on the PR; then the status `ladder / stuck` |
| 4 Ready | Every required check green, and a verdict of `ready` or `nits`, both at the current head | A step with no model in it | The commit status `ladder / ready`; the PR joins the maintainer's list of PRs ready to read |
| 5 Merge | Approve as code owner, then queue the PR; the merge queue lands it | The maintainer | The `main` ruleset: one approval, by a code owner, given after the last push; the merge queue |

Rungs 2 and 5 are enforced by GitHub. Rungs 3, 3b and 4 are shown, not
enforced: they tell the maintainer what state a PR is in, and his approval at
rung 5 is what holds the merge.

After rung 5 nothing changes. Release PRs, tags, deploys and repository
settings stay the maintainer's
([`guardrails.md`](guardrails.md#always-the-maintainers)).

An agent the maintainer starts himself, in a session on his machine, climbs
the same rungs with two differences. Rung 0 is the maintainer handing it a
triaged issue or a brief, so `agent-ready` is not needed. And its PR is
opened under his account for now, so rung 5 is the bypass described in
[Where things stand](#where-things-stand), not an approval.

## Lanes

A lane says how closely a pull request is read. A step with no model in it
sorts each PR into one lane from the paths it changes, using one file,
`agent/lanes.json` in `those-who-run/.github`:

- `light`: every changed path is documentation, or a test file the change
  adds. An edited, renamed or deleted test is not light, since that is where
  a weakened test hides.
- `standard`: a component's own code. Everything that is neither of the
  others.
- `sensitive`: any one path is among the ones that change what the rules or
  other repositories rest on: `.github/**`, `.claude/**`, `AGENTS.md`,
  `CLAUDE.md`, `CODEOWNERS`, the contracts' `proto/**` and `subjects.yaml`,
  `deploy/**`, a Dockerfile, a migration, a dependency manifest. One such
  file makes the whole change sensitive. `agent/lanes.json` holds the full
  list.

The lane sets how deep the review goes, and it leads the entry in the
maintainer's ready list, so he knows how closely to read. On a `sensitive`
PR the reviewer names each sensitive file, for the maintainer to read
himself.

A lane never changes who merges. Every PR in every lane waits for the
maintainer at rung 5. No model decides the lane, so a PR cannot describe
itself into a lighter one, and a repository can add sensitive paths of its
own but cannot declare its code light.

A build job refuses a diff that touches `.claude/**` or `CODEOWNERS`
outright: those change by the maintainer's hand or by the release fan-out,
not by a builder.

## Identities

Who each actor is on GitHub, and what it cannot do. The maintainer creates
the Apps and holds their keys; an agent never holds one.

| Identity | What it does | What it cannot do |
|---|---|---|
| `twr-builder`, a GitHub App (planned) | Pushes branches and opens pull requests, from the publish step of the build and fix jobs. Installed on every repository but `those-who-run/.github`. It writes contents and pull requests, and reads issues. | Approve a PR, merge one, or label an issue. It is not a code owner and is on no ruleset's bypass list. It cannot change a workflow until the maintainer grants it that. |
| `twr-reviewer`, a GitHub App (exists; posting since 2026-11-07) | Comments on pull requests and sets commit statuses, from the steps that pick up work and post results. Installed on every repository. It reads contents and issues. | Push, approve, merge, or label an issue. It is not a code owner and is on no bypass list. |
| `twr-release`, a GitHub App (exists) | What it does today: release-please's release PRs and tags, and the release fan-out's PRs. | Anything the ladder adds. It builds, reviews and approves nothing. |
| The maintainer | Rung 0 and rung 5. The only code owner, the only approver, the only admin. Agent sessions on his machine also work under his account for now. | Approve a PR his own account opened: GitHub does not allow it. |
| `mo-chara-croche`, a bot user account (exists) | Nothing on the ladder. Agent sessions worked as it for a time ([`guardrails.md`](guardrails.md#agent-identity)). Whether it is kept, for anything that needs a user account and not an App, is undecided. | Approve as a code owner, or bypass a ruleset: it has the Write role, never Admin. |

Because `twr-builder` is not the maintainer, the maintainer can approve its
PRs, and they land through the merge queue with every required check run on
what merges. That is the path the ruleset was written for. A PR opened under
the maintainer's own account cannot take it.

`twr-builder` is to be allowed to change workflows only once the maintainer
has checked three things: no repository or organisation Actions secret can be
reached from a branch, the default workflow token is read-only, and
`.github/**` is under code-owner review.

## The shape of an unattended job

Every unattended job, the reviewer's and the builder's alike, has the same
three steps, and the model is only in the middle one.

```
a scheduled step (no model) picks the work  ──▶  one job per unit of work

  fetch    no model, a read token      clones at a recorded commit; writes the issue,
                                       its comments and the lane out as data files
  agent    a model, NO GitHub token    runs Claude with the brief; writes files to an
                                       output folder
  publish  no model, a write token     validates the output folder, scans it, then
                                       pushes, comments or sets a status
```

- **A step with no model fetches.** It holds a token that can only read.
- **A step with a model and no GitHub token works.** It cannot push,
  comment, label or approve, whatever the text it reads tells it to do. It
  holds the login for Claude and nothing for GitHub.
- **A step with no model publishes.** It holds the write token, and it posts
  only what passes its checks.

So the model never holds a write credential, and the step that writes never
has a model in it. Each step makes a short-lived token from its App's key,
and the key is present in that step only.

What else follows from the shape:

- The briefs and the skills are read from `those-who-run/.github` at a
  pinned release, never from the PR's checkout, so a PR cannot change the
  reviewer that judges it.
- State lives on GitHub, in marker comments, statuses and labels. The step
  that picks up work keeps no database of its own.
- The jobs run on the maintainer's cluster, one at a time, and never while a
  race session is live.

What the scheduled step picks up:

- **Review:** an open PR by an allowlisted author (`twr-builder`, the
  maintainer, `twr-release`, Renovate) that is not from a fork, not a draft
  and not on `hold`, whose commits are all by allowlisted authors, and that
  has no review marker for its current head.
- **Build:** a paddock issue with `agent-ready` applied by the maintainer,
  without `needs triage` or `hold`, and with no open PR or agent branch for
  it already.
- **Fix:** a `twr-builder` PR whose verdict at the current head is
  `changes`, with fewer than two fix rounds behind it. A PR from a session
  on the maintainer's machine is left to that session.
- **Ready:** a PR with every required check green and a verdict of `ready`
  or `nits` at the current head.

What the publish step refuses:

- **A build:** a branch not cut from `origin/main`; a commit not authored as
  the App; a diff that touches `.claude/**` or `CODEOWNERS`; a test deleted
  or skipped without a `Tests weakened:` line; a secret or a private value
  found by the scans; a PR body without the template's sections or its
  `Closes` or `Part of` line.
- **A review:** a head commit that moved since the fetch, which is queued
  again; a comment longer than 15 KB; a private value found by the scan. A
  comment that is held goes to the maintainer, never to GitHub.

## The fix loop

When the verdict is `changes`, the builder answers the review: it checks
each item the reviewer asked for, then fixes it or contests it with a
reason, and replies once, item by item. The reviewer then reads the new
head. A review is evidence to check, not an order: the builder never
deletes, skips or loosens a test to reach `ready`, and never edits the
guard, the settings, the skills or the reviewer's brief.

The loop is limited to two rounds. The rounds are counted from markers on
the PR, not from anything an agent remembers. After the second answer, if
the verdict is still not `ready` or `nits`, the PR gets the status
`ladder / stuck` and waits for the maintainer, with what is still disputed
written down. No third round starts.

The limit is there because two agents can agree with each other and both be
wrong, or disagree for ever. For the same reason the reviewer never sees the
builder's session, only the PR.

## Why the verdict is advisory

`review / verdict` is not a required check, and is not meant to become one
as things are.

A required check has to report on every commit the ruleset tests, and with a
merge queue that includes the queue's own `merge_group` commit: the PR
merged onto `main` and the PRs ahead of it. The reviewer's status is set on
the PR's head commit. It does not exist on the queue's commit, so a ruleset
that required it would wait there for a check that never reports, and the
queue would stall.

So the verdict stays advice: a second read for the maintainer, never an
approval. What enforces the merge is his approval at rung 5, which the
ruleset does require. Whether the verdict should ever gate anything is the
maintainer's to decide once there are reviews to judge it by.

## What this means for an agent now

- Follow [`AGENTS.md`](../AGENTS.md). The ladder adds to it and weakens
  nothing in it.
- Rung 5 is the maintainer's on every PR, in every lane. An agent never
  approves a PR and never merges with a bypass. An unattended agent never
  merges, queues or enables auto-merge at all; an agent in a session does
  so only when the maintainer asks for that PR, on green
  ([`AGENTS.md`](../AGENTS.md#merging)).
- An unattended agent starts only on an issue carrying `agent-ready`,
  applied by the maintainer.
- Where a repository has the skills under `.claude/skills/`, they are the
  procedure for rung 1 ([`CLAUDE.md`](../CLAUDE.md)).
