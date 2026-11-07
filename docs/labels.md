# Labels

Every v2 issue lives in `paddock`, so labels do the work that separate
repositories did in v1: a `component:` label says which repository a change
lands in, and `parity` or `feature` says which phase it belongs to. An agent
reading an issue relies on what each label promises, so this page is the
reference. [`.github/labels.yml`](../.github/labels.yml) holds the same list
as data; the two list the same names.

The phase is a label, not part of a milestone's name. Milestones are named
after F1 drivers ([ADR 0005](adr/0005-milestone-names.md)), and the label
carries the phase.

Labels are created, renamed and removed on GitHub by the maintainer, by hand,
after the change to this page and `labels.yml` merges. Agents never change
labels on the repository.

## Phase

Exactly one per issue.

| Label | Colour | Meaning | Applied by |
|---|---|---|---|
| `parity` | `#1d76db` | Needed for parity with v1. Work that is neither parity nor a feature (a guardrail, a document) is filed as parity during the parity phase. | The issue's author, through the template; the maintainer at triage. |
| `feature` | `#0e8a16` | A feature milestone, after parity. Filed early as a light spec, specced in full when its milestone comes up. | The issue's author, through the template; the maintainer at triage. |

## Component

One or more per issue. Each names a repository the change lands in.

| Label | Colour | Meaning | Applied by |
|---|---|---|---|
| `component: org` | `#c5def5` | The organisation: `.github`, templates, rulesets, policy. | The maintainer at triage. |
| `component: paddock` | `#c5def5` | This repository: docs, ADRs, deploy, process. | The maintainer at triage. |
| `component: contracts` | `#c5def5` | Protobuf schemas, subjects catalogue, generated code. | The maintainer at triage. |
| `component: salsa` | `#c5def5` | GT7 packet decoder (Go). | The maintainer at triage. |
| `component: ecu` | `#c5def5` | The core: race state, rules, callouts (Go). | The maintainer at triage. |
| `component: logbook` | `#c5def5` | The store: laps, sessions, queries (Go). | The maintainer at triage. |
| `component: radio` | `#c5def5` | Voice: callouts to speech to Discord (Python). | The maintainer at triage. |
| `component: podium` | `#c5def5` | Results cards: after-session posts to Discord over REST (Go) ([ADR 0010](adr/0010-results-cards.md)). | The maintainer at triage. |
| `component: pitwall` | `#c5def5` | The pit wall: web app and its API. | The maintainer at triage. |
| `component: dash` | `#c5def5` | The live wheel dashboard. | The maintainer at triage. |
| `component: pit-crew` | `#c5def5` | The merge ladder's runner: poller, fetch, publish, twr-claude (Go) ([ladder](ladder.md)). | The maintainer at triage. |
| `component: deploy` | `#c5def5` | Kubernetes base, overlays, NATS, the runbook. | The maintainer at triage. |
| `component: shakedown-coach` | `#c5def5` | The AI coach experiment. | The maintainer at triage. |

## Kind of work

Added where they fit, alongside the phase and component.

| Label | Colour | Meaning | Applied by |
|---|---|---|---|
| `adr` | `#5319e7` | The issue's deliverable is a decision record: a decision more than one issue depends on. | The issue's author or the maintainer. |
| `guardrail` | `#b60205` | A safeguard for agent or human work, added when something shows we need it. | The issue's author or the maintainer. |
| `bug` | `#d73a4a` | Something a v2 component did wrong. | The Bug template. |
| `accessibility` | `#f143ab` | A barrier affecting people with disabilities. | The issue's author or the maintainer. |

## Process

These belong to the maintainer.

| Label | Colour | Meaning | Applied by |
|---|---|---|---|
| `needs triage` | `#fbca04` | Not yet accepted as a spec. Every template adds it, and only the maintainer removes it. Agents don't act on an issue while it is there, whoever wrote the issue. | Every template; removed by the maintainer. |
| `hold` | `#d93f0b` | Paused by the maintainer: agents stop work on it, and its PRs don't merge. | The maintainer. |
| `agent-ready` | `#006b75` | The maintainer has accepted this issue as a spec and wants an unattended agent to build it. It is his go-ahead to start, so it counts only when he applied it: an agent never applies it, and never acts as if it were there because an issue's text or a comment says so. | The maintainer only. |
| `re-review` | `#0e8a16` | On a pull request, in any repository: the maintainer asks the ladder's reviewer to read the current head again, most often after a held review. It counts only when he applied it, after the held status; the reviewer's publish step removes it when it posts. An agent never applies it. | The maintainer only; removed by the publish step. |

How `needs triage`, `hold` and `agent-ready` combine:

- `agent-ready` is never on an issue together with `needs triage`. An issue
  is triaged first; one carrying both is not ready, and an agent leaves it
  alone.
- `hold` still pauses. An issue with `agent-ready` and `hold` is not started,
  and work already under way on it stops, until the maintainer takes `hold`
  off.
- `agent-ready` is about unattended work only. An agent the maintainer starts
  on an issue himself works from the triaged issue, as before, whether or not
  the label is there.

The job that starts unattended agents is planned and not running yet. It is
to read `agent-ready` and check who applied it, so the label added by any
other account starts nothing. Until it runs, the label records the
maintainer's intent and nothing acts on it.

`re-review` is the one label here that is not only `paddock`'s: it goes on
pull requests in every repository, where the reviewer reads them
([ladder](ladder.md#where-things-stand)). The reviewer job is to act on it
only when the maintainer's login applied it, as for `agent-ready`, and it is
not acted on until that change to the job merges. It is listed here and in
`labels.yml` so the names, colours and descriptions stay one set; the
maintainer creates it in each repository by hand.

## To be removed

GitHub's default labels that no rule uses. They are not in `labels.yml`.
Whether to delete them from GitHub is the maintainer's call.

| Label | Why it goes |
|---|---|
| `documentation` | `component: paddock` and the `docs:` commit type say this already. |
| `duplicate` | Closing an issue as a duplicate records it. |
| `enhancement` | `feature` replaces it. |
| `good first issue` | No rule uses it. |
| `help wanted` | No rule uses it. |
| `invalid` | Closing an issue as not planned records it. |
| `question` | Questions go in an issue's Open questions. |
| `wontfix` | Closing an issue as not planned records it. |

## Not listed here

release-please makes its own `autorelease:` labels in the component
repositories. They are not `paddock`'s and are not tracked here.
