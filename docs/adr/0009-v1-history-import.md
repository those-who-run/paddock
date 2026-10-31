# 0009: v1's history is imported into `logbook` from its stored frames

**Status:** Accepted, 2026-10-31. Asked for by #263. Supersedes
[ADR 0004](0004-beside-v1.md), point 7.

## Context

[ADR 0004](0004-beside-v1.md), point 7, kept v1's sessions as an archive and
left an import to "a later decision … with a record of its own". Rosberg
puts the pit wall on v2's data, and the pit wall should show the
maintainer's whole history, as v1's does, not start empty at the switch.

v1 kept every lap's 60 Hz frames (`sessions/<id>/lap-NN.parquet`) beside its
rows (`index.sqlite`). So v2 can judge each old session itself, with the same
lap rules, track detection and session kinds as new data, and fill in v1's
unknowns instead of carrying them over.

The maintainer accepted this in the Rosberg triage (2026-10-26, on #263 and
#264) and settled the rest on 2026-10-31. This record carries both.

## Decision

### 1. v1's history is imported into `logbook`

v2's store holds v1's sessions beside its own, and `logbook` stays their
only writer and reader ([ADR 0003](0003-the-store.md), point 1). The import
happens now, in Rosberg, but off the critical path: the pit wall is built and
shipped on v2's own sessions, and does not wait for it.

Settles: #263 (R1.1), ADR 0004 point 7's open question.

### 2. Re-derived from v1's frames, through `ecu` as a library

- **`logbook import-v1`** (#264) turns each lap's stored frames back into
  frames and runs them through `ecu`'s own race state, the same code as live,
  with nothing reimplemented for the import. There is no NATS and no running
  stack.
- **`ecu`'s race state becomes public** (`race`, with its kinds and laps,
  plus `track`, `pit` and `fuel`, under `github.com/those-who-run/ecu/...`),
  because Go forbids importing another module's `internal/`. Everything
  else, adapters and rules included, stays `internal/`. #264 settles the
  exact list, on the principle "only what the importer needs".
- **v1's rows are used only** to find the sessions, to report differences,
  and for the sessions in point 5.

Settles: #263 (R1.2), #264 (R2, and where the race state lives).

### 3. Imported sessions are `src=v1-import`

They sit under their own source, beside `live`, `replay-<run>` and `parity`
([ADR 0001](0001-v2-architecture.md), point 2), so they never mix with v2's
own sessions and can be shown, filtered or dropped as a set. They are never
published on NATS: the source is a store value, not a subject.

Settles: #263 (R1.3), #264 (R3).

### 4. Their ids are v1's ids, as they are

An imported session keeps v1's id unchanged (for example `20261017-143142`).
One v1 session gives at most one imported session, under v1's id.

- **Old links keep working.** v1's results cards in Discord link to
  `/?session=<v1id>`, and once the v2 pit wall takes v1's address
  ([ADR 0011](0011-pit-wall-access.md), point 3) it answers them with the
  same id.
- **No clash.** v1's ids are a timestamp; `ecu`'s are ULIDs, a different
  length and alphabet, so one can never be taken for the other.
- **A second run finds them** by source and id, so the import can be
  repeated and changes nothing for a session already imported (#37's
  upserts).
- **When `ecu` ends, splits or discards it differently** from v1 (a restart,
  a next session started, telemetry lost, the pause limit, or a session it
  throws away), that session is imported from v1's rows, flagged as in
  point 5, and listed in the import's summary.

The importer sets the id itself. `ecu`'s machine mints ULIDs (`race.IDs` is
a `func() ulid.ULID`), so the importer rewrites the session id on every
event it stores.

Settles: #263 (R1.4), #264 (R3, "derived from v1's session id" read as v1's
id unchanged). The one-to-one rule is the maintainer's decision
(2026-10-31).

### 5. What is lost or approximate

- **Sessions stored before v1's frames carried every field v2 needs** are
  imported from v1's rows, as v1 judged them, and flagged (for example
  `derived: false`), so the whole history comes over.
- **Sessions `ecu` ends, splits or discards differently from v1** are
  imported from v1's rows, flagged (point 4).
- **Sessions with no lap files, or files that can't be read,** are left out.
- **Every session that differs** from v1's rows beyond the parity
  tolerances, and every one flagged or left out, is listed with its reason in
  the import's summary.

Settles: #263 (R3, and its open question).

### 6. When it runs

- **Once in Rosberg,** when #264 lands, on a copy of v1's data with
  `logbook` stopped, before the Rosberg demo (#136).
- **Again after any use of v1** once the v2 pit wall has taken v1's address
  ([ADR 0011](0011-pit-wall-access.md), point 3): until then, v1's new
  sessions are not found on v2's pit wall, and their cards' links 404.
- **A final run at Hamilton,** before v1's volume is archived (#114), to take
  whatever v1 raced since.

The rerun rule is the maintainer's decision (2026-10-31).

Settles: #263 (R1.5).

## What #263 does because of this

This record, its row in [`README.md`](README.md), and the "Superseded in
part" line on ADR 0004 are #263's R1 and R2. #264 builds the importer to it.

## Consequences

- **`ecu` and `logbook` releases are coupled.** `logbook` builds against
  `ecu`'s public packages, so a change to them is a release of `ecu` and a
  bump in `logbook`, like a contract.
- **`ecu`'s public packages are an API.** Moving code out of `internal/`
  makes its shape harder to change; only the race state moves, not the
  adapters.
- **Imported history is judged by v2.** Some old sessions may come out
  different from what v1 showed (a track named where v1 had none, a kind
  changed). The summary lists them; v2's answer stands.
- **Two kinds of id in one store,** v1's timestamps and `ecu`'s ULIDs. The
  pit wall treats both as opaque.
- **Imported ids can never change.** Cards in Discord carry them, so a
  session's id is permanent once it is imported.
- **The pit wall answers `/?session=<id>` for good,** on every address cards
  have used ([ADR 0011](0011-pit-wall-access.md), point 3), whatever its
  routes become.
- **It relies on the bare session id.** The pit wall's API takes the session
  id alone, with `src` kept out of URLs (#121, #124); a change there would
  break old cards.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **Keep v1's history as an archive** (ADR 0004, point 7). The pit wall
  would start empty at the switch, and old card links would break.
- **Convert v1's rows as they are.** Cheaper, but it carries v1's unknowns
  over and judges old sessions differently from new ones.
- **A Python importer reusing v1's code.** It would judge sessions by v1's
  rules, not v2's, which is what parity is for.
- **New ids for imported sessions,** for example a ULID or a `v1-` prefix.
  Every old card link would need a redirect table.

## References

- #263 and #264, and the maintainer's triage comments on them (2026-10-26).
- v1's store: https://github.com/those-who-run/those-who-run/blob/main/docs/pitwall.md
