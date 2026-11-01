# 0012: One pit wall address

**Status:** Accepted, 2026-10-31. The maintainer's decision, for #132 and
#136. Supersedes [ADR 0011](0011-pit-wall-access.md), point 3.

## Context

[ADR 0011](0011-pit-wall-access.md), point 3, moved the pit wall in four
steps: v2's pit wall on a new host first, `podium`'s cards linking to it, then
v2 taking v1's address once the import was checked, and v1's pit wall retiring
last. Both addresses were to serve v2's pit wall for good, because cards would
link to each.

That is a second host to keep for good (a name, a certificate, an Ingress) and
a stretch with two pit walls showing two different histories. No card links
to a new host yet, because none exists. The maintainer chose one address
instead (2026-10-31).

## Decision

### 1. One address: v1's

v2's pit wall is deployed on the address v1's pit wall has now. There is no
new host. `podium`'s `TWR_PITWALL_URL` is that address. The host stays the
private overlay's; the base names none ([ADR 0011](0011-pit-wall-access.md),
point 1).

Settles: #132 (one Ingress host, v1's), and where #136's demo runs.

### 2. v1's pit wall comes down as soon as v2's is deployed

v1's pit wall is taken down when v2's pit wall is deployed, in the same
change to the overlay or the one right after it. It doesn't wait for the
import. Only v1's pit wall goes: v1's engineer is already stopped, and its
removal stays with #114.

Settles: #132, and #114 R5's pit wall half (gone before Hamilton).

### 3. v1's data is kept

v1's stored data (its volume and the files on it) stays as it is. #264's
import reads it ([ADR 0009](0009-v1-history-import.md), point 6), and so does
the final run at Hamilton before the volume is exported and freed (#114).
v1's sessions in v1's own pages are still possible by running v1's pit wall
image by hand over the volume (#114 R4).

### 4. Old card links keep resolving

`/?session=<id>` on that one address is permanent, with the bare session id
and `src` kept out of URLs (#121, #124). v1's cards find their session once
it is imported under v1's id ([ADR 0009](0009-v1-history-import.md),
point 4), and v2's cards find theirs from the start.

## What changes in the other records

- **ADR 0011, point 3,** is replaced by this record. Its status line and its
  row in [`README.md`](README.md) say so. Points 1, 2 and 4 hold.
- **ADR 0009,** points 4 and 6 and its consequences, say "once the v2 pit
  wall takes v1's address". That is now the day v2's pit wall is deployed.
  The rerun rule is unchanged: after any use of v1, the import runs again.

## Consequences

- **One host, one certificate, one Ingress** for the pit wall, for good.
- **Between the deployment and the first import run,** a v1 card's link
  reaches v2's pit wall and finds no session. Running the import (#264) soon
  after the deployment keeps that short.
- **No v1 pit wall on the cluster to compare with.** Parity is checked on
  recordings (#133), and the import's summary lists every session that
  differs from v1's rows.
- **The way back is cheap.** v1's data is kept, so if v2's pit wall fails,
  the overlay can put v1's pit wall back on the same address.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **ADR 0011's two addresses.** It keeps v1's pit wall up until the import
  is checked, but it costs a second host for good, and cards would link to
  two addresses that both must stay.
- **One address, but v1's pit wall kept until the import is checked.** It
  avoids the gap for old links, but v2's pit wall would need somewhere else
  to run in the meantime, which is the new host again.

## References

- [ADR 0011](0011-pit-wall-access.md), point 3, which this replaces.
- [ADR 0009](0009-v1-history-import.md), points 4 and 6.
- #132, #136, #114, #264.
