# 0005: Milestones are named after F1 drivers

**Status:** Accepted, 2026-10-22. Asked for by #11.

## Context

In v1 the milestones were numbered, and from 2026-10-17 each was a release
whose name began with the version it would ship as. The numbers held only
while the order did. Cockpit voice was 0.4 before 2026-10-17, 0.7 that day
and 0.13 the next, when v1's nine larger releases were split into small ones;
0.11 went from Progression to Wheel dashboard, and Progression became 0.22
and 0.23. Each move meant renaming milestones and editing the issues, notes
and roadmap rows that named the old number, and older issues and PRs still
use numbers that now mean something else (see v1's
[roadmap](https://github.com/those-who-run/those-who-run/blob/main/ROADMAP.md),
"Earlier milestones").
v2 has about twenty milestones across about ten repositories, and each
repository versions on its own. A number in a milestone's name would be wrong
within weeks, and a version in it would belong to no repository in
particular.

## Decision

### 1. A milestone is named after an F1 driver

Each milestone takes the name of an F1 driver, chosen for what the milestone
delivers, not for its place in the order: Brundle, the driver who became the
voice of F1, is the voice milestone. The reason for each name is in the
"Why the name" column of [`ROADMAP.md`](../../ROADMAP.md#the-order). A name
can change until work on its milestone starts, and never after.

Settles: #11, and the names in #4.

### 2. `ROADMAP.md` is the single source of truth for the order

The table under [The order](../../ROADMAP.md#the-order) in `ROADMAP.md` is the
only place the order of the milestones lives. Reordering is moving a row. No
milestone name carries a number, no GitHub milestone has a due date, and the
project board has no order field. The position column in the table is the
current position, not part of the name, and is stored nowhere else.

Settles: #4 (where the order lives).

### 3. Milestones are not versions

Each component versions on its own with release-please, and a milestone
says nothing about any component's version. When a milestone closes, its
description may say which component versions closed it, written after the
fact. "v2.0" is another name for the swap (Hamilton), not a tag on any
repository.

Settles: #4 (the rule that milestones are not versions).

### 4. Milestones live in `paddock`

Every milestone is a milestone on
[`paddock`](https://github.com/those-who-run/paddock/milestones), where every
v2 issue lives; component repositories have none. Each has a one-line
description: the outcome, and a link to `ROADMAP.md` for the order.

Settles: where the milestones named in #4 are created.

### 5. Parity or feature is a label

The phase is the `parity` or `feature` label
([`docs/labels.md`](../labels.md)), not part of the milestone's name, so a
milestone can move between phases without a rename.

Settles: #3 (why the phase is a label).

### 6. A name is never reused

A name belongs to one milestone, even after that milestone closes. If a
milestone is dropped, its name is retired with it and not given to another.
An old link or note that names a milestone therefore always means the same
one.

Settles: #11.

## What #11 does because of this

#11 is this record and its row in [`README.md`](README.md).
[`ROADMAP.md`](../../ROADMAP.md) links here from its Rules, and
[`docs/labels.md`](../labels.md) already does.

## Consequences

- **The order changes with one commit:** a row moves in `ROADMAP.md`, and no
  milestone, issue or note needs a rename.
- **Finding what's next means reading `ROADMAP.md`,** not sorting milestones
  on GitHub. Milestones there sort by name or by how complete they are, which
  says nothing about the order.
- **Release notes come from each component's changelog,** which
  release-please writes from the squashed commits. A milestone has no release
  notes of its own; its closing description points to the versions instead.
- **A name says what a milestone is about only to someone who knows the
  reason for it,** so the outcome always travels with the name: in the
  milestone's description and in the roadmap's table.
- **The list of usable names shrinks:** a dropped or closed milestone uses up
  its driver for good.

## Alternatives considered

- **Numbered milestones tied to versions,** as in v1. Rejected for the
  reasons in Context.
- **Numbered milestones not tied to versions** (M1, M2 …). The numbers still
  read as an order, so they lie as soon as a row moves.
- **Plain descriptive names** (Foundation, Walking skeleton). They work, but
  two outcomes drift towards the same words over twenty milestones, and a
  short, distinct name is easier to say and to search for. The outcome stays
  beside the name instead.
