# The walking skeleton on a laptop

[`compose.yaml`](compose.yaml) runs v2's walking skeleton on one machine, with
no PS5 and no cluster (#39): NATS with JetStream, the stream and KV bootstrap
from [`deploy/nats/`](../nats/README.md), `logbook`, `ecu replay` for
replaying a v1 recording onto the bus, `radio` for hearing it, and `nats-box`
for looking at it.

## What you need

- Docker with compose v2 (`docker compose version`). `podman compose` works
  too: run the targets as `COMPOSE='podman compose' make up`.
- `make` and `curl`.
- On an arm64 machine (Apple Silicon), emulation of amd64 in Docker Desktop:
  v2 images are built for `linux/amd64` only
  ([ADR 0006](../../docs/adr/0006-images-on-ghcr.md)), and `compose.yaml` asks
  for that platform for `logbook` and `replay`. NATS and `nats-box` are multi-platform.
- Ports 4222, 8222 and 8080 free on 127.0.0.1.

## The commands

Run them from the root of `paddock`. Each prints the commands it runs. They
name the compose project, `twr`, with `-p`, so `COMPOSE_PROJECT_NAME` does not
change it.

| Command | What it does |
|---|---|
| `make up` | Starts `nats`, runs the bootstrap once it is healthy, starts `logbook` once the bootstrap has exited 0, then waits up to 60 s for `logbook`'s `/healthz` to answer 200. `nats-box` is not started |
| `make replay FILE=… [SPEED=1] [RUN=compose]` | Runs `ecu replay --speed SPEED --run RUN /recordings/FILE` once in the `replay` service, onto the bus as source `replay-RUN`, and exits with `ecu`'s exit code: 0 once everything is published. `FILE` is a file in `recordings/`; `SPEED` is `1` (the recording's own pacing), any factor (`10`) or `max`. It starts NATS and the bootstrap first if they are not running; `logbook` stores the replay once it runs |
| `make radio [SINK=console] [RUN=compose] [WAV=.cache/radio-wav]` | Starts `radio` in the background on source `replay-RUN`'s callouts, as your own user. `SINK=console` prints a line a callout in its log (`docker compose -p twr -f deploy/compose/compose.yaml logs -f radio`); `SINK=wav` also writes a numbered WAV file a callout to `WAV`. Start it before `make replay`: `CALLOUTS` keeps a callout 30 s. `make down` stops it |
| `make sub` | Prints every message on `twr.>` through `nats-box`, until Ctrl-C: frames on `twr.v1.<src>.frame.gt7` and `.frame.gt7.lo`, events on `twr.v1.<src>.evt.<session>.<type>` |
| `make export [SRC=live]` | Stops `logbook`, prints the stored laps of source `SRC` (`logbook export --data /data --laps --src SRC`) as JSON lines, and starts it again, without rerunning the bootstrap |
| `make down` | Stops and removes the containers; the volumes, and with them the streams and `logbook`'s data, stay |
| `make reset` | `down`, and removes the volumes: the next `make up` starts empty |

A replay, from the root of `paddock`, with the stack up:

```sh
make up
make sub          # in another terminal, to watch the bus
make replay FILE=real-race-2laps-2026-10-14.jsonl.gz SPEED=max
make export SRC=replay-compose
```

To hear it, start `radio` first:

```sh
make up
make radio SINK=wav
make replay FILE=real-race-2laps-2026-10-14.jsonl.gz
ls .cache/radio-wav    # 001-<rule>.wav, ...
```

`radio` uses `simulated` timing, on the replay's own clock, so it says what a
driver would have heard at any `SPEED`; `TWR_RADIO_TIMING=instant` says
every callout. It speaks to no Discord here.

`make export` prints the replay's four laps: laps 7 and 8 of the practice
(the second incomplete) and laps 1 and 2 of the race. `ecu replay`'s session
ids are new on each run, so a second replay under the same `RUN` stores its
sessions again beside the first; pass another `RUN`, or `make reset` first.
`ecu`'s README has the replay's flags, its clock and its exit codes.
`TWR_RECORDINGS` mounts another directory at `/recordings` instead of
`recordings/`; it is resolved from `deploy/compose/`, so give it absolute.

## The services

| Service | Image | What |
|---|---|---|
| `nats` | `nats:2.15.1-alpine` | `-js -sd /data -m 8222`, JetStream on the `nats-data` volume; healthy once `/healthz?js-enabled-only=true` answers |
| `nats-bootstrap` | `natsio/nats-box:0.20.0` | [`bootstrap.sh`](../nats/bootstrap.sh) with `deploy/nats` mounted read-only, as user 1000 on a read-only root file system, as the cluster's Job runs it (#43). It runs on every `make up` and changes nothing when the streams and buckets are already right |
| `logbook` | `ghcr.io/those-who-run/twr-logbook:0.1.4` | `logbook run` as user 65532 on a read-only root file system, its store on the `logbook-data` volume at `/data`; `linux/amd64` |
| `replay` | `ghcr.io/those-who-run/twr-ecu:0.1.6` | Profile `replay`: only `make replay` or `docker compose run replay replay …` starts it. `ecu`'s own entrypoint, so its command is `ecu`'s arguments; `TWR_NATS_URL` names the server. As user 65532 on a read-only root file system, the recordings (`TWR_RECORDINGS`, `recordings/` by default) read-only at `/recordings`; `linux/amd64`. The parity harness ([`parity/`](../../parity/README.md)) runs it with `--no-deps` and its own recording mounted at `/recording/` |
| `radio` | `ghcr.io/those-who-run/twr-radio` | Profile `replay`: only `make radio` or `docker compose up radio` starts it. The callouts of `TWR_RADIO_SRC` (`replay-compose` by default) to `TWR_RADIO_SINK` (`console` by default, or `wav` into `TWR_RADIO_WAV` at `/wav`), with `simulated` timing. As `TWR_UID:TWR_GID` (65532 by default; `make radio` passes yours) on a read-only root file system, its speech cache on a tmpfs at `/tmp`; `linux/amd64` |
| `nats-box` | `natsio/nats-box:0.20.0` | Profile `tools`: only `make sub` or `docker compose run` starts it |

Every service drops all Linux capabilities and runs with `no-new-privileges`,
and none uses the host's network. Every image is pinned by version tag and
digest, never `latest`. Renovate's
docker-compose manager bumps the tag and digest together
([`renovate.json`](../../renovate.json) turns on `pinDigests` for it).
The NATS images are the ones [`scripts/nats_it.sh`](../../scripts/nats_it.sh)
tests the bootstrap with, and `tests/test_compose.py` fails when the two
differ.

| Port on 127.0.0.1 | What |
|---|---|
| 4222 | NATS clients |
| 8222 | NATS monitoring (`/healthz`, `/varz`, `/jsz`) |
| 8080 | `logbook`'s `GET /healthz` |

Nothing is published beyond 127.0.0.1. `logbook`'s `/healthz` answers 503
until it is connected and both its consumers are bound, and its JSON carries
`laps_written` since start. Its image is distroless, with no shell or HTTP
client, so compose cannot check it from inside the container: `make up` asks
from the host with [`wait-healthy.sh`](wait-healthy.sh).

## logbook's data

`logbook` writes its store to `/data` in its container, which is the
`twr_logbook-data` volume (`docker volume inspect twr_logbook-data` says where
it is on the host). It survives `make down` and is removed by `make reset`.

To read it, use `make export`. `logbook export` takes the store's lock and
refuses while `logbook` runs, so the target stops `logbook`, runs the export
against the same volume and starts `logbook` again; the frames and events
published meanwhile wait in the streams. It prints the laps of source `live`
(what `ecu` stores listening to the PS5) unless `SRC` names another:
`make replay` publishes as `replay-compose`, or `replay-RUN`.

## The same as the cluster

What the cluster also needs is the same here, so the kustomize base copies it
rather than reinventing it: the definitions in `deploy/nats` and the script
that applies them, the users and read-only root file systems of the bootstrap
and `logbook`, every capability dropped and no new privileges, the
environment variable names (`NATS_URL`, `TWR_NATS_URL`, `TWR_DATA_DIR`,
`TWR_HEALTH_ADDR`) and `logbook`'s data at `/data`.

## Checks

- `tests/test_compose.py` (in `make test`) reads `compose.yaml`: every image is
  pinned by tag and digest and none is `latest`, every port is published on
  127.0.0.1 only, no service sets `network_mode`, `privileged`, `cap_add`,
  `pid` or `ipc`, every service has an `image:` and no `build:`, drops all
  capabilities and sets `no-new-privileges`, the bootstrap, `logbook`,
  `replay` and `radio` have read-only root file systems, `replay` keeps `ecu`'s entrypoint
  and mounts the recordings read-only, `radio` speaks to the console by default
  with no Discord setting, and the NATS images match
  `scripts/nats_it.sh`'s.
- `make compose-it` ([`scripts/compose_it.sh`](../../scripts/compose_it.sh),
  in `make check` and CI's `test` job) starts from `make reset`, runs
  `make up`, checks that only `nats` and `logbook` run and the bootstrap exited
  0, checks NATS's `/healthz` with JetStream, that the bootstrap made
  `CALLOUTS`, `TELEMETRY`, `EVENTS` and the `live` and `ecu` buckets, and that `logbook` reports
  connected, and runs `make export` against the running stack. Then it runs
  `make replay FILE=real-race-2laps-2026-10-14.jsonl.gz SPEED=max`, waits
  until `logbook`'s `/healthz` counts the 4 laps v1 stored for it written and
  stays there for 2 s (so a fifth lap would be stored and seen), checks it
  wrote exactly 4, and checks that `make export SRC=replay-compose` prints those 4 (AC2). Then
  `make reset`, and it checks that no container or volume of the `twr` project
  is left.
