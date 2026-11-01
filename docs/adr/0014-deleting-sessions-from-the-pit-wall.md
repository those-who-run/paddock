# 0014: Deleting sessions from the pit wall

**Status:** Accepted, 2026-11-01. The maintainer's decision, for #362.

## Context

The pit wall shows every session `logbook` holds for the sources it is set
to, and until now it only read them. A session that should not be there (a
test drive, a few laps in the wrong car, a session a bad import made) stays in
the list, its filter counts and the same-track comparisons for good. The
maintainer asked for a way to delete a session from the pit wall
(2026-11-01, #362).

That is the pit wall's first write, and it touches several records at once:
[ADR 0003](0003-the-store.md) (`logbook` is the only writer of the store),
[ADR 0011](0011-pit-wall-access.md) (the pit wall has no login, on the home
network only), and the contracts both sides build against
([ADR 0002](0002-contracts.md)). This record settles it once, for the
`contracts`, `logbook`, `pitwall` and `paddock` changes.

## Decision

### 1. A delete is soft, and can be undone

`logbook` marks the session deleted, with the time it was deleted
(`deleted_at`), and keeps all its rows and its lap files. Nothing is removed
from the volume.

A delete from a page with no login has to be cheap to take back: a wrong
click, or someone else on the home network, must not cost a session. Keeping
everything makes the delete a flag, so the way back is one more request, and
the store never has to rebuild what it threw away.

`deleted_at` survives any other write to the session. When `logbook`
upserts it again, from a redelivered event
([ADR 0003](0003-the-store.md), point 5) or a rerun of the v1 import
([ADR 0009](0009-v1-history-import.md), point 6), the session stays
deleted: only a restore (point 3) clears it. `logbook`'s tests pin it.

A purge, which removes a session's rows and files for good, is not part of
this. It is a follow-up if it is ever wanted.

Settles: #362 (AC1: the data and lap files stay on disk).

### 2. Where a deleted session shows

- **Left out:** `sessions.list` (the list, and its filters' values and
  counts) and `sessions.same_track`.
- **Still answered, with `deleted_at` set:** `sessions.get`, `laps.list` and
  `laps.trace`.

A results card's link (`/?session=<id>`, [ADR 0012](0012-one-pit-wall-address.md),
point 4) is permanent, so it still opens a deleted session, which the page
shows as deleted with a way to restore it.

Settles: #362 (AC1, hidden from the list and same-track; AC2, a deleted
session's link still opens it).

### 3. Restore, and both are idempotent

Restoring a session clears `deleted_at`, and nothing else does (point 1). Deleting a session that is already
deleted, or restoring one that is not, succeeds and changes nothing, so a
request sent twice, or from two tabs, is harmless.

Settles: #362 (AC2, Restore brings it back; AC4).

### 4. What can't be deleted

- **An open session** (one not yet closed): `failed_precondition`. Its rows
  are still being written, and the engineer is still in it.
- **A session that does not exist:** `not_found`.

Settles: #362 (AC3).

### 5. `logbook` applies it; `ecu` is not involved

A delete is the driver tidying the history, not a decision about the race,
so it does not go through `ecu`, and no event is published for it. The pit
wall asks `logbook`, and `logbook` applies the request and nothing else. It
stays the only writer of the store ([ADR 0003](0003-the-store.md), point 1).
This is not the session's lifecycle, which stays `ecu`'s
([ADR 0001](0001-v2-architecture.md), point 3).

`deleted_at` is a change to `logbook`'s schema, so it comes in a new
migration with the next number. A merged migration is never edited.

Settles: #362 (What changes: logbook, the new migration).

### 6. The store's contract

`contracts` gains two request/reply endpoints in `subjects.yaml`, beside
`svc.logbook.sessions.get`:

- `svc.logbook.sessions.delete`
- `svc.logbook.sessions.restore`

Each names its session the way `sessions.get` does, and the pit wall is
their only caller. The store's `Session` message gains `deleted_at`.

Settles: #362 (What changes: contracts, the two endpoints and
`deleted_at`).

### 7. The pit wall's API

`PitwallService` gains `DeleteSession` and `RestoreSession`:

- **They are POST.** They have side effects, so they do not carry
  `NO_SIDE_EFFECTS`, and Connect serves them only as POST, never as GET.
- **They are never cached,** and after either one the list's ETag changes,
  so a browser holding the list asks again and does not keep showing a
  session that has gone, or miss one that is back.
- **`GetSession`,** and whatever else the session page uses, carries
  `deleted_at`.
- The service's comment no longer calls it read-only.

Settles: #362 (What changes: contracts and pitwall, the two POST
methods).

### 8. No login: ADR 0011 stands

[ADR 0011](0011-pit-wall-access.md), point 1 (home network only, no login)
holds for this write too:

- **It is on the home network only.** Whoever can delete a session could
  already read all of them.
- **It can be undone** (point 1). The worst a stray delete does is hide a
  session until someone restores it.
- **Cross-site requests are refused.** A page on another site can't make the
  browser delete a session. Connect takes a request body only as JSON or
  protobuf, and a cross-site POST with either content type needs a CORS
  preflight first. The pit wall answers no preflight, so the browser never
  sends the request. A plain form or an image tag can't reach the delete
  either: a form can't set those content types, and point 7 keeps both
  endpoints off GET. `pitwall`'s tests for the two endpoints check that a
  request with another content type is refused, and that a preflight gets no
  CORS answer.
- **DNS rebinding gets around that, and is an accepted risk.** A page on a
  rebinding domain is the same origin as the pit wall once its name points
  at it, so its JSON POST needs no preflight, and the pit wall does not
  check `Host`. It is accepted because a delete can be restored, nothing
  leaves the disk (point 1), and the pit wall is on the home network only
  ([ADR 0011](0011-pit-wall-access.md), point 1). If it is ever wanted, the
  fix is an allowlist of `Host` values on the write methods, set by the
  overlay. Schumacher's review under ADR 0011, point 4, is where it would be
  weighed.
- **NATS's WebSocket can't reach it.** The `ws` user, the one a browser
  gets through the WebSocket Ingress, may not publish
  ([`deploy/nats/chart/values.yaml`](../../deploy/nats/chart/values.yaml)),
  so it can't send a request to `logbook`. Only clients in the cluster's
  namespace can, as for every other `svc.logbook.*` endpoint. That holds
  with the `network-policy` component on, as a cluster that enforces
  NetworkPolicy includes it
  ([`deploy/README.md`](../../deploy/README.md#the-base)).

[ADR 0011](0011-pit-wall-access.md), point 4, already sends the access
question back to Schumacher's spec before the setup pages ship. That review
covers this write as well.

Settles: #362 (AC5).

### 9. The web app

- The session page has a Delete button, with a confirmation that names the
  session: its track, car and date.
- After a delete, the page shows the session as deleted, with a Restore
  button.
- The list no longer shows it.

Settles: #362 (AC1 and AC2, the session page's Delete and Restore).

### 10. The Discord card stays

Deleting a session does not touch `podium` or Discord: a card already posted
stays, and its link opens the session as deleted (point 2). Taking the card
down too is a follow-up if it is wanted.

Settles: #362 (Out of scope: the Discord card).

### 11. paddock

paddock: this record. NATS's permissions do not change: in the base every
client in the namespace is NATS's `twr` user, which may do anything
([`deploy/nats/chart/values.yaml`](../../deploy/nats/chart/values.yaml)), so
the pit wall can already request the two new subjects (as it can any
other); compose runs NATS with no users. The namespace is the boundary only
with the `network-policy` component on, as a cluster that enforces
NetworkPolicy includes it
([`deploy/README.md`](../../deploy/README.md#the-base)). Compose and parity
keep working as they are.

`deploy/nats/subjects.yaml` is a copy of `contracts`' at a tag, which
`scripts/nats_check.py` compares by hash, so the two new endpoints reach it
when paddock copies a `contracts` release that has them, not before.

Settles: #362 (What changes: paddock).

## What #362 does because of this

This record and its row in [`README.md`](README.md) are #362's paddock part.
NATS's permissions need no change (point 11), so paddock's PR changes no
rendered manifest. `deploy/nats/subjects.yaml` takes the two endpoints when paddock
next copies a `contracts` release that has them. The acceptance criteria are
met by the `contracts`, `logbook` and `pitwall` PRs; AC5 by `pitwall`'s tests
(point 8).

## Order

`contracts` (`store.proto`, `pitwall.proto`, `subjects.yaml`) merges and is
released first. `logbook` and `pitwall` then bump to that tag, each in its own
PR ([`AGENTS.md`](../../AGENTS.md), "Across repositories"). This record and
paddock's changes go beside `contracts`.

## Consequences

- **A deleted session takes space for good,** until a purge exists. A session
  is small next to the volume, so that is accepted.
- **The pit wall writes.** It still holds no state of its own and mounts no
  volume; it asks `logbook`, as it does for every read.
- **Anyone on the home network can delete a session,** and anyone can
  restore it. Accepted, as for reading under ADR 0011.
- **A deleted session is out of sight but not gone.** There is no page
  listing deleted sessions; one is reached by its link, such as a card's.
- **Parity** compares only what both pit walls show, and nothing deletes a
  session in a parity run, so it is unchanged.

Changing any point here needs a new record that supersedes this one.

## Out of scope

- A purge (hard delete), of the rows, the lap files or both.
- A page listing deleted sessions (a bin).
- Deleting several sessions at once.
- Taking down or editing a session's Discord card.

## Alternatives considered

- **A hard delete.** Simple, but a wrong click on a page with no login would
  lose a session for good, and the only way back would be a backup.
- **A login for writes only.** It means building the login ADR 0011 chose
  not to, for a change that can already be undone. Schumacher's review
  (ADR 0011, point 4) is where a login gets weighed.
- **Deleting through `ecu`,** as an event `logbook` applies. A delete is not
  a race decision, and an event would put the driver's tidying into the
  session's history, which every replay would then carry.

## References

- [ADR 0001](0001-v2-architecture.md), point 3 (`ecu` owns the session).
- [ADR 0003](0003-the-store.md), point 1 (`logbook` owns all writes) and
  point 5 (idempotent writes).
- [ADR 0009](0009-v1-history-import.md), point 6 (when the v1 import runs).
- [ADR 0011](0011-pit-wall-access.md), points 1 and 4, and
  [ADR 0012](0012-one-pit-wall-address.md), point 4.
- [ADR 0002](0002-contracts.md) (how a contract evolves) and
  [ADR 0008](0008-contracts-tooling-and-tags.md).
