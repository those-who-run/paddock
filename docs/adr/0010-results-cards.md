# 0010: Results cards are posted by `podium`

**Status:** Accepted, 2026-10-26. Asked for by #65.

## Context

In v1 the voice bot posts the results cards. `ResultsPoster` borrows
`DiscordPlayer.client` ("one login, no privileged intents"), and the store and
the poster call each other in a loop: `store.on_end = poster.submit`, and
`on_posted = store.mark_posted`. A card is one more job inside the process
that speaks.

v2 breaks that loop with events: the card is a subscriber of `session.ended`
([ADR 0001](0001-v2-architecture.md), point 3, "every feature is a subscriber
and fails alone"). Where that subscriber lives was left open: ADR 0001,
point 4, says "where results cards are posted from is a later record (#65)",
and [ADR 0003](0003-the-store.md) left open whether after-session jobs run
inside `logbook` or as their own subscriber. Button's other issues (#66,
#67) and #61 R6 depend on the answer, so it is recorded once.

The maintainer decided it in the Button triage on 2026-10-26
([the decision](https://github.com/those-who-run/paddock/issues/65#issuecomment-6413276096)).
This record carries that decision.

## Decision

### 1. Cards are posted by their own subscriber, `podium`

The results card is posted by a component of its own, not by `radio`.

| Repository | What | Language | Ships as | Label |
|---|---|---|---|---|
| `podium` | Results cards, and later every other post made after a session | Go | `ghcr.io/those-who-run/twr-podium` ([ADR 0006](0006-images-on-ghcr.md)) | `component: podium` |

- **Go,** the default from ADR 0001, point 5. v1's `build_card` is Python, but
  a card is small, and the formatting it needs is shared with the pit wall
  through the `contracts` `format` package (#128) rather than copied from v1.
- **It subscribes to `session.ended`** and reads anything else it needs
  through `logbook`'s services, never its volume (ADR 0003, point 1).
- **Posted sessions are kept in KV,** so a redelivered `session.ended` posts
  nothing (#67).

The repository exists, made from the chassis template.

Settles: #65 (R2), #66 and #67 (where they land), ADR 0001 point 4's open
line, and ADR 0003's question on after-session jobs, for cards.

### 2. It reaches Discord over REST, with the second bot's token

A card needs only Discord's REST API (create a message with an embed),
authenticated with the bot token. `podium` opens no gateway session.

- **The token is the second bot's,** the one `radio` uses
  ([ADR 0004](0004-beside-v1.md), point 6). Two processes holding one token
  is fine here: REST calls and `radio`'s voice gateway connection do not
  fight, as only a second gateway session with the same token would.
- **No privileged intents and no gateway library.** `podium` is an HTTP
  client, which keeps it small.

Settles: #65 (R3).

### 3. `podium` is the home for everything posted after a session

The debrief, the PB card and the weekly digest
(those-who-run/those-who-run#102, those-who-run/those-who-run#105,
those-who-run/those-who-run#106) go in `podium` when they are built, so those
features do not start another repository. So does any other line written to
the results channel, such as #61 R6's warning about an empty voice channel:
`radio` publishes an event and `podium` posts it.

Settles: #65 (R5); #61 R6 (who posts the line).

## What #65 does because of this

This record and its row in [`README.md`](README.md) are #65's R1. #66 and
#67 are titled `feat(podium): …` and labelled `component: podium` (#65 AC2).
The label is added to [`../labels.md`](../labels.md) and
[`.github/labels.yml`](../../.github/labels.yml).

## Consequences

- **A card fails alone.** A Discord REST outage or a bad card cannot touch
  voice, and `radio` crashing mid-race cannot lose a card: `session.ended`
  waits in the stream until `podium` takes it.
- **One more repository, image and deployment.** Its own CI, releases and
  manifests, and its place in the base. The maintainer accepted the cost.
- **One token in two processes.** Rotating the second bot's token means
  restarting both `radio` and `podium`, which read the same Secret.
- **The switch scales one more piece.** When v2 is live, `podium` is one of
  its live pieces beside `ecu` and `radio` (ADR 0004, point 1), so the
  switch (#109, Alonso) scales "the card subscriber", which is `podium`.
- **`radio` stays voice only.** It never posts to a text channel; anything it
  wants written there is an event for `podium`.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **Cards inside `radio`, sharing its Discord client,** as v1 does. One
  login and no new repository, but a card failure or a Discord REST outage
  shares a process with voice, a crash in either takes both down, and `radio`
  would grow a second job in a different language from the default.
- **Cards inside `logbook`, on a job runner,** as v1's
  those-who-run/those-who-run#189 did. `logbook` already has the data, but
  the store would gain a Discord client and a second reason to fail, and
  ADR 0001 keeps each feature its own subscriber.
- **`podium` on a gateway session of its own,** through a Discord library.
  Not needed to post a message, and a second gateway session on the same
  token would fight `radio`'s; a third bot would avoid that but adds a token
  to manage for nothing a card needs.
- **Python, to reuse v1's `build_card`.** Saves porting one function, but
  against the languages decision, and the card's formatting is shared with
  the pit wall in Go through `contracts` (#128).

## References

- #65, and the maintainer's triage comment on it (2026-10-26).
- v1: `src/those_who_run/results.py` (`ResultsPoster`), `app.py`
  (`store.on_end = self.poster.submit`), and
  [the engineer's docs](https://github.com/those-who-run/those-who-run/blob/main/docs/engineer.md)
  (Results channel).
- Discord, create message: https://discord.com/developers/docs/resources/message#create-message
