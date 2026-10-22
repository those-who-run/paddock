# 0004: v2 runs beside v1

**Status:** Accepted, 2026-10-22. Asked for by #10.

## Context

v1 ([those-who-run/those-who-run](https://github.com/those-who-run/those-who-run))
is frozen at 1.0.x, for fixes only, and the maintainer races on it the whole
time v2 is built. v2 grows beside it, piece by piece, and takes over only when
it matches v1 on recordings ([0001](0001-v2-architecture.md), point 7). Until
then both have to be deployed at once without getting in each other's way, and
the move from one to the other has to be cheap in both directions.

Some things only one engineer can hold:

| Resource | Why there is only one |
|---|---|
| The PS5's telemetry | GT7 streams to whoever sends it heartbeats, on UDP 33740. Both engineers want that port on the host |
| A Discord bot's voice connection | One token holds one voice connection. Two engineers on one token fight over the channel |
| Session data | Not shared. v1 keeps its own store and volume; `logbook` has its own (ADR 0003, #9). Nothing to conflict |

So "which version is live" means "which engineer is running". Everything
else can run on both sides at once.

Lauda (the base and the runbook), Alonso (the switch) and Hamilton (the swap)
all build on how the two sit side by side. This record decides it once, so v1
stays untouched and the swap is one commit, with the way back the same commit.

How our own components are packaged was weighed for this record, because the
switch and the public-base, private-overlay split both depend on it:

| | Kustomize | Helm |
|---|---|---|
| What it is | Plain YAML, with patches and overlays | Templates and a values file, packaged as a chart |
| A public base with a private overlay | What overlays are for | A values file per environment; the private parts become values |
| The v1 and v2 switch | One component per engineer; the overlay includes one | One `engineer:` value and `if` blocks in the templates |
| Reading and reviewing | What is in git is what is applied | The output is hidden until `helm template` renders it |
| Packaging for other people | Weak | Its strength, but nobody else installs this |
| Third-party software | Can inflate a chart (`helmCharts`), which Argo CD has to allow | Native |

## Decision

### 1. Only one engineer is live at a time

The PS5 streams to one listener on UDP 33740, and one Discord token holds one
voice connection (Context). At any moment exactly one engineer is live: it
holds the PS5's stream and is the one the driver hears in voice. The other
side holds neither. v2's live pieces are `ecu`'s live frame source, `radio`
speaking in Discord and the results card subscriber posting cards; v1's is
its engineer.

Settles: #106 and #108 (`ecu` holds the port only when v2 is live), #109
(what each side scales).

### 2. The switch lives in the deployment, not in v1

- **One value decides it:** `engineer: v1 | v2`. `paddock`'s base has one
  kustomize component per value, and the environment's overlay includes one.
- **Each component** scales and configures one side's live pieces up and the
  other side's down. Which one the overlay includes is the value.
- **Flipping is one commit** in the environment's overlay, which Argo CD
  rolls out. Rolling back is the same flip.
- **No NATS and no standby mode in v1.** v1's engineer is never changed except
  its replica count: same image, same configuration, same volume. Flipping
  back restores it exactly.

The names of the components, the CI check that exactly one is included, and
the order in which one side goes down and the other comes up are #109's.

Settles: #109, #112 (the Alonso demo).

### 3. Never mid-race

- **The idle gate checks whichever engineer is going away.** v1 has had it
  since those-who-run/those-who-run#272: `/healthz` reports `idle` and
  `idle_secs`, and a rollout waits until the engineer has been idle for a
  while. In v2 the same gate runs before any sync that would restart or switch
  off the live engineer, whichever side that is.
- **`ecu` reports idle** in its entry in the `live` KV and on its own
  `/healthz`, with the same meaning as v1's.
- The gate never needs a change in v1. Its threshold, timeout and override are
  #110's.

Settles: #107 (`ecu`'s idle signal), #110 (the gate on both sides).

### 4. What stays up either way

NATS, `logbook`, the pit wall and the dash run whichever engineer is live.
With v1 live they are quiet, because nothing live is on the bus, and they can
be used with replays at any time: a replay publishes under its own
`replay-<run>` source ([0001](0001-v2-architecture.md), point 2), and `radio`
speaks to a WAV or console sink, or its own test channel, and reaches the
driver only when v2 is live.

Settles: #45 (replays on the cluster), #60 (the sinks for replays), #109 R4.

### 5. Its own namespace and Argo CD application

v2 runs in its own namespace, `twr`, beside v1's, and has its own Argo CD
Application, built from `paddock`'s base and an environment's overlay. v1's
namespace and application stay as they are until Hamilton.

Settles: #44 (the base's namespace), #48 (the example Application), #54 (the
Lauda demo, beside v1).

### 6. A second Discord bot for v2

v2's `radio` speaks as a second bot, with its own token. While v1 is live it
uses only its own test channel; which channel it speaks in when v2 is live is
#109's. v2 can be tested in voice without touching v1's bot, and a shadow run
(below) could have both speaking at once. The token is a Secret named in the
base and provided by the overlay.

Settles: #58, #63.

### 7. v1's history stays an archive

v1's sessions stay in v1's store, read through v1, and are archived with it.
`logbook` starts with v2's own data. Some of v1's history is marked with
unknowns (tracks and session kinds v1 could not name), so v2 builds clean data
of its own. Importing v1's sessions into `logbook` is a later decision, if it
is ever made, and would need a record of its own.

Settles: #10's open question, by leaving it open.

### 8. Kustomize for our components, Helm only for third-party charts

- **Our components are kustomize,** as the table in Context weighs: plain
  YAML that agents can write and the maintainer can review as it will be
  applied, with the switch as one component per engineer.
- **Third-party software comes from its official Helm chart,** NATS first,
  with its values in `paddock`.
- **`paddock` holds the public base,** which assumes no particular cluster.
  Each environment's specifics (host names, storage, secrets, the namespace if
  it differs, replica counts, the Discord channels, and v1's own manifests)
  live in a private overlay outside `paddock`.

Examples in `paddock` use placeholders, never a real environment: an overlay
at `overlays/my-cluster/`, a host such as `nats.<host>`, an address such as
`192.0.2.10`, and Secrets by name and key, never by value.

Settles: #42 (NATS from its chart), #44, #47 (the example overlay), #50 (the
portable-base check), #53 (how to write a private overlay).

### 9. The swap

When parity is reached (Hamilton):

1. Race on v2 for a while, with v1 one commit away.
2. Then v1's deployment is removed, the switch with it, and v1's repository
   is renamed `parc-ferme` and archived.

Settles: #113, #114, #115, #116, #117, #118, #119.

## Later option: a live shadow run

If replay parity is not convincing enough, a small UDP relay could heartbeat
the PS5 and fan each packet out to both engineers, with v1 pointed at the
relay through its existing `GT7_PS5_IP` setting and `radio` silent or
speaking through the second bot. That should need only configuration in v1,
and #111 checks it: v1 also sends its heartbeat to that address, so the relay
has to absorb it, and the relay and v1 cannot both hold UDP 33740 on one host
address. This is not part of the plan. It is held in #111, which decides
whether to build it at all.

## What #10 does because of this

#10 is this record and its row in the README. Lauda's, Alonso's and
Hamilton's issues refer to it for the namespace, the switch's value and
`ecu`'s idle signal, and their bodies agree with it.

## Consequences

What this costs:

- **v1's engineer is touched from v2's deployment,** if only its replica
  count. Its manifests live in the private overlay (point 8), and one Argo CD
  application has to own its Deployment, so the two applications never fight
  over it. #109 says how.
- **A hand `kubectl scale` does not stick.** Argo CD's self-heal reverts it
  within minutes, so the hurry path in the runbook is a commit, or pausing the
  application's auto-sync first (#109 R7).
- **Two bots and two tokens** to keep until Hamilton, and their channels,
  however #109 settles them.
- **Two stores** until Hamilton, and v1's history never shows in v2's pit wall
  unless a later record imports it.
- **Kustomize does not package v2 for anyone else.** If that is ever wanted, a
  chart can come then.

## Alternatives considered

- **A toggle in v1,** a standby mode or a NATS publisher in the frozen code.
  It changes the version being frozen, and the way back would depend on new
  v1 code.
- **v1 ingests and republishes to v2,** or v2 ingests and v1 consumes. Both
  need new code in v1, or make v1 depend on v2 to run.
- **Helm for everything,** one chart with an `engineer:` value. Templates
  hide what is applied, and the public and private split ends up as values
  files (Context).
- **v2 in v1's namespace.** Cheaper, but the two would share names, Secrets
  and policies, and removing v1 at Hamilton would mean picking its pieces out
  one by one.

## References

- v1's idle gate: those-who-run/those-who-run#272
- v1's ADR 0001, data volume and network:
  https://github.com/those-who-run/those-who-run/blob/main/docs/adr/0001-data-and-network.md
- v1's shadow engineer: those-who-run/those-who-run#274
- Kustomize components:
  https://kubectl.docs.kubernetes.io/guides/config_management/components/
- Argo CD with Helm: https://argo-cd.readthedocs.io/en/stable/user-guide/helm/
- NATS Helm chart: https://github.com/nats-io/k8s/tree/main/helm/charts/nats
