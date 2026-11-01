# 0013: The v1 import fills what v1's frames lack from v1's summary

**Status:** Accepted, 2026-11-01, by its merge: the maintainer's decision, for
#264. Supersedes [ADR 0009](0009-v1-history-import.md), point 2, third bullet
("v1's rows are used only to find the sessions, to report differences, and for
the sessions in point 5").

## Context

[ADR 0009](0009-v1-history-import.md) re-derives v1's history from v1's stored
frames through `ecu`'s race state, and keeps v1's rows for finding the
sessions, reporting differences and the sessions that can't be re-derived
(point 5).

Building the importer (#264) showed that v1's lap files
(`sessions/<id>/lap-NN.parquet`, v1.0.0's `FRAME_COLUMNS`) never carried six of
the fields `ecu` reads on every frame:

- the lap total;
- GT7's last lap time and best lap time;
- the fuel tank's capacity;
- the field's size;
- the cars-on-track flag.

v1 kept the frames it needed for its pit wall's charts, and read these six from
the packet as it went, so it never stored them. Read as it stands, point 5
("sessions stored before v1's frames carried every field v2 needs are imported
from v1's rows") covers every v1 session, and re-deriving would never happen.

## Decision

### 1. Before `ecu` re-derives a session, each frame is made whole from v1's summary

`logbook import-v1` fills the six fields on each frame from the session's
`summary.json`:

- **the lap total, the tank's capacity, the field's size and the cars-on-track
  flag:** the session's own values, the same on every frame;
- **GT7's last and best lap times:** made from the lap times v1 stored. On a
  frame of lap N, the last is v1's time for the latest timed lap before N, and
  the best is the quickest of those, or -1 before there is one, as GT7 sends
  them.

The frames then go through `salsa`'s v1 mapping and `ecu`'s race state
unchanged, as ADR 0009 point 2 says. `ecu` then judges:

- **the kind and the start**, from the frames plus three of the filled
  values: the lap total, the field's size and the cars-on-track flag. These are
  the kind's inputs, so the kind is only partly v2's own judgement.

From the frames alone, it judges:

- the track;
- the pit stops and each lap's part in them;
- the positions;
- the fuel;
- the finish.

Settles: #264 (how a session with v1's lap files is re-derived at all).

### 2. A summary that lacks a source goes the rows path

When `summary.json` lacks a value the frames would be filled from, the session
is taken from v1's rows, flagged, as point 5 says, and the import's summary
gives the reason ("summary.json lacks …"). The values that can be lacking are:

- `fuel_capacity` or `total_cars`, which v1's summary has only from 2026-10-16;
- a usable lap total;
- a time for each complete lap of a timed session.

`ecu` is never run on a made-up value.

Settles: #264 (sessions from before v1's summary carried the fields).

## What it hides

A re-derived session is v2's judgement of v1's frames, but not all of it:

- **Lap times and lap attribution are v1's.** `ecu` closes and times a lap
  from the last lap time the frame carries, and that time is v1's. A
  re-derived session's lap times can only equal v1's, so the summary says
  "lap times are v1's", and comparing them checks the import, not `ecu`.
- **The time each lap is posted is idealised.** GT7 posts a lap's time a few
  frames after the line, and not always on the same frame. Here it arrives on
  the first frame of the next lap.
- **The kind is judged on filled inputs.** The lap total, the field's size and
  the cars-on-track flag are v1's, so `ecu`'s kind and start rest on them as
  well as on the frames. Comparing the kind with v1's checks that `ecu` reads
  the same inputs the same way, not the inputs themselves.
- **Values within a session are flattened.** The field's size, the lap total,
  the tank and the cars-on-track flag are the same on every frame. A field that
  changed during a session (a car leaving, say) isn't seen.
- **A missing source sends the session down the rows path** (point 2), so
  older sessions are v1's judgement, flagged, not v2's.

What the import checks against v1's rows, independently of v1's judgement, are
the track, the finish, the complete laps and the fuel. It also compares the
kind, which was judged from the frames plus v1's lap total, field size and
cars-on-track flag. The summary lists each
difference ([ADR 0009](0009-v1-history-import.md), point 5).

## Consequences

- **Most of v1's history is re-derived:** the track, pit stops, positions,
  fuel and finish are v2's, as ADR 0009 wanted. The kind and start are v2's
  reading of v1's lap total, field size and cars-on-track flag, together with
  the frames.
- **Lap times can't disagree** between v1 and v2 for imported sessions. Parity
  on recordings (#133), not the import, is where `ecu`'s lap timing is checked.
- **The importer reads v1's rows for more than ADR 0009 allowed:** to fill the
  frames, as well as to find, report and stand in.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **Every session from v1's rows** (ADR 0009 point 5 read literally). It is
  honest about what v1 stored, but no session is judged by v2 and the import
  through `ecu` buys nothing.
- **Infer the six fields from the frames.** For example, the field's size
  from positions seen, and lap times from the time between line crossings.
  That reimplements part of `ecu` in the importer, which ADR 0009 point 2 rules
  out, and the inferred values could be wrong in ways nothing would show.

## References

- [ADR 0009](0009-v1-history-import.md), points 2 and 5.
- #264, and the review of logbook#33.
- v1.0.0's `src/those_who_run/store.py`, `FRAME_COLUMNS` and `end_session`.
