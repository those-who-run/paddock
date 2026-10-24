# The board

v2 is planned on one organisation project,
[Race Control](https://github.com/orgs/those-who-run/projects/2). It shows
where each issue stands across every milestone and component, and the pull
requests in the component repositories show on it through the issues they
name. It shows state only: the milestone order lives in
[`ROADMAP.md`](../ROADMAP.md#the-order) and nowhere else
([ADR 0005](adr/0005-milestone-names.md)), so the board has no order field, no
due dates and no iterations.

v1 is not on it. v1 is in maintenance and is tracked by its own issues and
milestones.

## How an issue gets there

Every new or updated `paddock` issue is added on its own, by the project's
auto-add workflow, with the status **Needs triage**. The issues that existed
before the project were added once, in bulk, each with the status its labels
gave it then. Issues in other repositories don't exist (Issues are off there),
so nothing else needs adding.

## Status

| Status | Means | Moved by |
|---|---|---|
| Needs triage | New, and not yet a spec. The issue has the `needs triage` label ([`ISSUE_FORMAT.md`](ISSUE_FORMAT.md#needs-triage)). | The auto-add workflow. |
| Ready | Accepted as a spec and free to start. | The maintainer, on removing `needs triage`. A project workflow can't set a status from a label, so this step is by hand. |
| In progress | Someone is working on it. | The maintainer. |
| In review | Its pull request is open. | The maintainer. |
| Done | Closed. | The project's "Item closed" and "Pull request merged" workflows. |

Agents never change the project. An agent reads the board but works from
the issue itself, and only when it no longer has `needs triage`
([`AGENTS.md`](../AGENTS.md#untrusted-input)).

## Views

- **The current milestone:** its issues as a board by status, the view to
  check what is in flight now.
- **By milestone:** every issue in a table grouped by milestone. Groups are
  sorted by name or not at all, never in roadmap order.
- **By component:** every issue by its `component:` label, so it shows which
  repository the work lands in ([`labels.md`](labels.md)).
- **Guardrails and ADRs:** the issues labelled `guardrail` or `adr`.
