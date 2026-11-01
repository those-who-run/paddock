# 0015: The live engineer, by hand

**Status:** Accepted, 2026-10-31. The maintainer's decisions on #108, #109 and
#110. Supersedes [ADR 0004](0004-beside-v1.md), points 2 and 3, and its
consequence "A hand `kubectl scale` does not stick".

## Context

[ADR 0004](0004-beside-v1.md) planned the move from v1's engineer to v2's as
something done often and safely by machinery:

- point 2, one kustomize component per engineer (`engineer: v1 | v2`), a CI
  check that exactly one is included, and a flip made by one commit;
- point 3, an idle gate that runs before any sync that would restart or
  switch off the live engineer, with a threshold, a timeout and an override;
- a consequence that a hand scale does not stick, so the hurry path was a
  commit or pausing auto-sync.

Since then:

- v1's engineer was retired on 2026-10-29. v2 (`ecu` and `radio`) has been the
  live engineer since, on every race, and v1's engineer is at 0 replicas. A
  flip is now rare: only if v2 ever has to step aside.
- A restart mid-race no longer loses the race. Since those-who-run/ecu#56
  (ecu 0.1.11), a stopped `ecu` keeps its open session in the `ecu` bucket, and
  the next run continues it within the continue window, 600 s by default. It
  was checked live on 2026-10-31: two restarts, on lap 2 and on lap 3,
  continued one session. A restart on lap 1 starts a new session, as in v1.
- The live `ecu` runs from the private overlay, and the base keeps its rule of
  no host port.

Components, a CI check and a gate hook would be machinery for a flip that may
never happen again. The maintainer chose to do it by hand (2026-10-31).

## Decision

### 1. The switch is a rare flip, done by hand

There are no engineer components and no CI check. A flip scales the
Deployments by hand, in the order paddock's runbook gives
([`deploy/README.md`](../../deploy/README.md#switch-engineers)): the engineer
going away down first, and its pod gone, before the other comes up; to v2,
`ecu` and then `radio`; back to v1, `radio` first, so that it lets go of the
Discord bot. v1's engineer is still only ever scaled, never otherwise changed.

The scale is made to stick by the overlay's Argo CD Application:
`ignoreDifferences` on `ecu`'s and `radio`'s `/spec/replicas`, with
`RespectIgnoreDifferences=true`, so self-heal leaves a hand scale alone. v1's
engineer, under its own Application, needs the same or its scale made as a
commit.

[ADR 0004](0004-beside-v1.md), point 1, still holds: one engineer is live at
a time. The host port keeps two off one node: a second engineer on UDP 33740
stays `Pending`.

Settles: #109 (the runbook's "Switch engineers" section only), #112 (the
demo is a race on v2 as deployed, with no flip).

### 2. No gate hook: don't roll out while racing

There is no idle gate before a sync. The rule instead: don't roll out `ecu`
or `radio` while racing, whether an image bump, a configuration change or a
merge to paddock's `deploy/` that a cluster following `main` picks up. Check
first by hand: `/healthz` `idle` and `idle_secs` well over 300 s (#107).

It relies on ecu's session continuation: if the rule is broken, an `ecu`
restart from lap 2 on continues the session, one on lap 1 starts a new one,
and a `radio` restart can miss callouts while it reconnects to voice.

Settles: #110 (docs only, beside #109's section).

### 3. The live `ecu` is the overlay's copy of paddock's example

paddock's base has no `ecu` and no host port, and keeps that rule. paddock
carries an example of the live `ecu`,
[`deploy/examples/ecu-live`](../../deploy/examples/ecu-live/kustomization.yaml):
one replica replaced by `Recreate`, UDP 33740 as a `hostPort`, the PS5's
address as the placeholder `192.0.2.10`, its recordings on a claim and its
`/healthz` probes. The overlay copies it and sets its own values: the PS5's
address, the storage class, the node. `make deploy-render` checks the example
with the base's rules but that one host port (`scripts/deploy_check.py`,
`ecu_live`).

Settles: #108.

## What changes in the other records

- **ADR 0004, points 2 and 3,** and its consequence "A hand `kubectl scale`
  does not stick", are replaced by this record. Its status line and its row in
  [`README.md`](README.md) say so. Its other points hold.

## Consequences

- **No machinery to keep** for a flip that may not happen: no components, no
  CI check, no hook Job.
- **Nothing stops a rollout mid-race.** It is a rule, not a check. A mistake
  costs at most a split session on lap 1 or a few missed callouts.
- **A flip is a few minutes by hand,** and is checked only when it is used.
  If v1 is ever brought back, the runbook is followed word for word and
  corrected where it was wrong.
- **With `ignoreDifferences`, the replica count in git applies only when the
  Deployment is created.** After that, the count is the one set by hand.

Changing any point here needs a new record that supersedes this one.

## Alternatives considered

- **ADR 0004's components, CI check and gate hook.** Right for frequent flips
  with both engineers in use, which ended on 2026-10-29.
- **The live `ecu` in the base.** It would put a host port and a network's
  address in the public base, which every cluster then has to undo.
