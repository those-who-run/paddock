# The board

v2 is planned on one organisation project,
[Race Control](https://github.com/orgs/those-who-run/projects/2). It shows
where each issue stands across every milestone and component. A pull request
in a component repository shows on it, under the issue's linked pull
requests, only when it closes that issue (`Closes those-who-run/paddock#N`);
a `Part of …` pull request is not linked. It shows state only: the milestone order lives in
[`ROADMAP.md`](../ROADMAP.md#the-order) and nowhere else
([ADR 0005](adr/0005-milestone-names.md)), so the board has no order field, no
due dates and no iterations.

v1 is not on it. v1 is in maintenance and is tracked by its own issues and
milestones.

## How an issue gets there

Every new or updated `paddock` issue is added on its own, by the project's
auto-add workflow, with the status **Needs triage**. The open issues that
existed before the project were added once, in bulk, each with the status its
labels gave it then. Issues are turned off in the other repositories, so
nothing else needs adding.

## Status

| Status | Means | Moved by |
|---|---|---|
| Needs triage | New, and not yet a spec. The issue has the `needs triage` label ([`ISSUE_FORMAT.md`](ISSUE_FORMAT.md#needs-triage)). | The auto-add workflow. |
| Ready | Accepted as a spec and free to start. | The maintainer, on removing `needs triage`. A project workflow can't set a status from a label, so this step is by hand. |
| In progress | Someone is working on it. | The maintainer. |
| In review | Its pull request is open. | The maintainer. |
| Done | Closed. | The project's "Item closed" workflow, including when a pull request that closes the issue merges. The board holds only issues, so its "Pull request merged" workflow has nothing to act on. |

An issue whose status is Needs triage but which no longer has the label has
been triaged and not moved. The filter
`status:"Needs triage" -label:"needs triage"` finds them, and should show
nothing.

Agents never change the project. An agent reads the board but works from
the issue itself, and only when it no longer has `needs triage`
([`AGENTS.md`](../AGENTS.md#untrusted-input)).

## Views

- **Milestone Fangio:** the current milestone's issues as a board by status,
  the view to check what is in flight now. It is renamed, and its filter
  changed, when the next milestone starts.
- **By Milestone:** every issue in a table grouped by milestone. Groups are
  sorted by name or not at all, never in roadmap order.
- **By Component:** every issue by its `component:` label, so it shows which
  repository the work lands in ([`labels.md`](labels.md)).
- **Guardrails and ADRs:** the issues labelled `guardrail` or `adr`.
