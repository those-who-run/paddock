# Issue format

Every v2 issue lives in `paddock` and is a spec: a person or an agent should
be able to pick it up and build it without asking around first. The work
usually lands in another repository, the component the issue names, so the
issue has to carry everything that repository's PR needs.

There are two kinds of spec, each with its template in
`.github/ISSUE_TEMPLATE/`: **Parity** for work that brings v2 level with v1,
and **Feature** for work filed early and specced in full later. A third
template, **Bug**, reports something a v2 component did wrong. This page says
what goes in each section; the templates only hold short reminders.

## Rules

- **Evidence over assumption.** GT7 sends no mode, no wear and no flags; what
  it does send is learned from recordings. Every claim about telemetry names
  the recording it came from (outing id or fixture, and date) and the numbers
  seen. Anything not checked against a recording says **Unverified:** in front
  of it.
- **Recordings are the referee.** Recordings are pruned, so an outing an issue
  relies on is archived before it goes and cited by its archive id, marked
  *archived* (v1's
  [ADR 0002](https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0002-outing-archive.md)
  describes the archive).
- **Requirements are numbered** (R1, R2, …) and each says MUST or SHOULD.
  They describe behaviour, not implementation.
- **Acceptance criteria are checkboxes**, and each one is testable. A Parity
  criterion names the v1 recording or fixture it replays, or says "a v1
  recording" when any will do.
- **Approach is advice, not a contract.** It names where to start; the
  implementer may do better and says so in the PR.
- **One issue, one deliverable, one PR.** Split anything that needs two PRs,
  or two repositories.
- **Dependencies are kept in both directions** (Blocked by and Blocks), so
  the order of work can be read straight from the issues.
- **Shared decisions go in an ADR.** When more than one issue depends on the
  same decision, record it in `paddock`'s `docs/adr/` and link it from each
  issue, rather than deciding it in each.
- **Size:** S (an afternoon) or M (a day or two). An L is split before it is
  accepted.
- **Name symbols, not line numbers.** Line references drift as code changes.
  Point at a function, type or file (`decodePacket` in `internal/packet/decode.go`),
  and let the reader find it with `grep`.
- **The body is the current spec.** Findings that arrive later (a new
  recording, a test that changed the picture, the maintainer's answer to an
  open question) go in a comment and are then folded into the body.

## Parity

The full spec, for work in a parity milestone.

| Section | What it holds |
|---|---|
| Summary | One or two sentences: what changes, and in which component. |
| Why | The problem or the value, and the v1 behaviour it carries over. |
| Requirements | R1… with MUST or SHOULD. Behaviour, not implementation. |
| Out of scope | What this issue leaves to other issues, linked. |
| Interfaces | What it adds or changes that another issue may touch (see [Interfaces](#interfaces)). |
| Approach | A suggested design: packages, events, thresholds, data flow. Advice. |
| Acceptance criteria | Checkboxes, each testable, naming the v1 recording or fixture they replay. |
| Dependencies | Blocked by and Blocks, as issue links. |
| Open questions | Decisions still to make, and who makes them. |
| References | v1 code and issues, ADRs, prior art, related PRs. |

Work that is neither parity nor a feature (a guardrail, a document, the
organisation's CI) is filed as Parity while the project is in its parity
phase.

## Feature

The lighter spec, filed early so nothing from v1's backlog is lost. It is
specced in full, in the Parity sections, when its milestone comes up.

| Section | What it holds |
|---|---|
| Summary | One or two sentences: what the feature does. |
| Why | The problem or the value. |
| From v1 | The v1 issues it replaces, each linked with a one-line summary. |
| What changes in v2 | Which component or subscriber does the work, and which events it needs. |
| Open questions | Decisions still to make, and who makes them. |
| Dependencies | Blocked by and Blocks, as issue links. |

## Interfaces

Interfaces lists everything the issue adds or changes that another issue may
also touch, so overlaps between parallel specs show before they are built:

- NATS subjects, and the events on them with their `ce-type`;
- KV buckets and their keys;
- stream names;
- NATS micro endpoints;
- HTTP routes;
- configuration keys and environment variables;
- images and their tags.

Write "None" if there are none.

## Labels and milestone

Each issue has:

- exactly one milestone;
- exactly one of `parity` or `feature`;
- at least one `component:` label, naming the repository the change lands in;
- `adr` when the deliverable is a decision record, and `guardrail` when it is
  a safeguard for agent or human work.

[labels.md](labels.md) says what every label means and who applies it.

## Issues and pull requests across repositories

The issue lives in `paddock`; the PR lives in the repository of the
component it changes. The PR body says:

- `Closes those-who-run/paddock#N.` when every acceptance criterion is met;
- `Part of those-who-run/paddock#N` while any acceptance criterion is left,
  with which ones and why (for example, one that needs the maintainer's own
  car or phone).

A PR in `paddock` itself uses the same full form, so every PR reads the same.

## Needs triage

Every template adds the `needs triage` label. The maintainer removes it when
the issue is accepted as a spec, the maintainer's own issues included. Until
it is removed, agents don't work on the issue, whoever wrote it.

## Writing

- British English, plain sentences.
- Numbers carry their units: `3 s`, `20 Hz`, `120 °C`.
- The repositories are public. No host names, IP addresses, kube contexts or
  tokens: use placeholders such as `<host>` and `my-cluster`, and
  documentation addresses such as `192.0.2.x`.
