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

| View | Layout | Filter | Shows |
|---|---|---|---|
| Milestones in flight | Board, a column per status | `milestone:` and each milestone in flight, comma-separated in roadmap order, such as `milestone:Brabham,Lauda` | What is in flight now. |
| By Milestone | Table, grouped by milestone | none | Every issue under its milestone. |
| By Component | Table, with the Labels column | none; `label:"component: ecu"` for one component | Which repository each issue's work lands in. |
| Guardrails and ADRs | Table | `label:guardrail,adr` | The issues labelled `guardrail` or `adr`. |

- **Milestones in flight:** a milestone is in flight from when its first
  issue starts until its last one closes, and several often are at once.
  The view keeps its name; its filter lists every milestone in flight, so the
  maintainer adds a milestone when its first issue starts and takes it out
  when its last closes. Each column holds the issues of every milestone in
  the filter: to see one, replace the filter with `milestone:Lauda` in the
  filter bar for a while, without saving the view. The columns are right only
  while the project's workflows set Needs triage and Done
  ([Status](#status)). If those workflows are off, closed issues stay under
  their last status, so the view's filter adds `is:open` and shows only the
  work still to do.
- **By Milestone:** groups are sorted by name or not at all, never in roadmap
  order, which lives only in [`ROADMAP.md`](../ROADMAP.md#the-order).
- **By Component:** GitHub can't group a view by label, so this view is a
  table with the Labels column, where each issue's `component:` label shows
  the repository its work lands in ([`labels.md`](labels.md)). Filter it by
  one `component:` label to see that repository's issues, such as
  `label:"component: ecu"`.
- **Guardrails and ADRs:** the safeguards and the decisions, whatever their
  milestone ([`guardrails.md`](guardrails.md), [`adr/`](adr/README.md)).

The project's README says what each view is for in a line, and links here
and to [`ROADMAP.md`](../ROADMAP.md).
