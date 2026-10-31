# 0011: Who can reach the pit wall

**Status:** Accepted, 2026-10-31. Asked for by #134.

## Context

v1's pit wall is reachable on the home network with no login. v1's
those-who-run/those-who-run#385 asked whether that should stay, and never got
an answer. In v2 it matters more: the code is public, the pit wall and the
dash are separate apps, and the dash ships read-only NATS credentials in its
bundle, which [ADR 0001](0001-v2-architecture.md), point 6, accepts "on a
private network only". Schumacher will later put the PS5 address and Discord
settings behind pages. The pit wall's deployment (#132) should build to an
answer, not an assumption.

The options, as #385 listed them, and what each means:

| | A: LAN only, no login | B: one shared login | C: an identity provider | D: a VPN from outside |
|---|---|---|---|---|
| The pit wall's Ingress | A host on the LAN, no auth | Basic auth at the Ingress, or a login in the pit wall | Forward auth at the Ingress, and an identity provider to run | As A or B inside; nothing published outside |
| The dash's NATS user and the WebSocket host | As ADR 0001: credentials in the bundle, the WebSocket host on the LAN | The WebSocket host needs the same login, or stays LAN only; the bundle's credentials are still readable by anyone who loads the dash | As B, through forward auth on the WebSocket host | As A: outside devices are on the private network |
| The `live` KV's readers | Anyone on the LAN | Whoever has the login, if the WebSocket is behind it | Named users | As A |
| The setup pages (Schumacher, and v1's admin token, those-who-run/those-who-run#140) | Need a guard of their own | Behind the login, which everyone shares | Can check who the user is | As A |

The maintainer leaned to A for parity, with D as the way in from outside
(Rosberg triage, 2026-10-26, on #134), and accepted it on 2026-10-31.

## Decision

### 1. LAN only, no login, as v1

The pit wall, the dash and NATS's WebSocket are reachable on the home
network only, with no login. The base publishes nothing outside: each
Ingress gets the overlay's own host, which is a private name (for example
`pitwall.<host>`). ADR 0001, point 6, holds as written.

Settles: #134 (R1, R3), #132 (it builds to A; no follow-up issues).

### 2. From outside, through a VPN

When the pit wall is wanted from outside the home network, the way in is a
VPN onto it (option D), not a public Ingress or a login. The VPN is the
private overlay's concern and needs no change in the base. Nothing is built
for it now.

Settles: #134 (R1, D named for later).

### 3. The pit wall's address

1. **The v2 pit wall gets a new host first,** beside v1's.
2. **`podium`'s `TWR_PITWALL_URL` points to it at once,** so new results
   cards link to v2's sessions.
3. **It takes v1's pit wall address once the import is checked**
   ([ADR 0009](0009-v1-history-import.md)), so old cards' links
   (`/?session=<v1id>`) reach v2's pit wall and find the imported session.
4. **Then v1's pit wall retires.**

The hosts are the overlay's; the base names none.

Settles: where the pit wall lives while v1's history moves over, for #132
and `podium`'s deployment.

### 4. Schumacher revisits it

The setup pages change what is behind the pit wall: settings that steer the
engineer, not only history. Schumacher's spec revisits this record before
they ship, with B and C as the options to weigh, and the admin token
(those-who-run/those-who-run#140) either way.

## What #134 does because of this

This record and its row in [`README.md`](README.md) are #134's R1 to R3. The
answer is A, so R4 has no follow-up issues. #134 AC3 (v1's #385 links here)
is done when Hamilton closes v1's issues (#116).

## Consequences

- **Anyone on the home network can read everything,** the history, the live
  KV and the dash's credentials, as with v1. Accepted while it is a private
  network.
- **No login to build or run** for Rosberg, and no identity provider.
- **Outside access waits for a VPN,** which is the maintainer's setup, not
  the base's.
- **Two pit wall hosts for a while,** until v1's address moves over (point 3).

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **B, one shared login.** Little to run, but a shared password is weak, and
  the dash's credentials are still in its bundle.
- **C, an identity provider.** Real users, but one more service to run and
  keep up for a single driver.
- **A public Ingress with a login,** without a VPN. It puts the pit wall and
  NATS's WebSocket on the internet for a feature nobody needs yet.

## References

- #134, and the maintainer's triage comment on it (2026-10-26).
- v1: those-who-run/those-who-run#385, those-who-run/those-who-run#140 and
  those-who-run/those-who-run#252.
- [ADR 0001](0001-v2-architecture.md), point 6 (the dash reads NATS).
