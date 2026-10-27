# The walking skeleton on a laptop

[`compose.yaml`](compose.yaml) runs v2's walking skeleton on one machine, with
no PS5 and no cluster (#39): NATS with JetStream, the stream and KV bootstrap
from [`deploy/nats/`](../nats/README.md), `logbook`, and `nats-box` for
looking at the bus. Replaying a recording through `ecu` comes with ecu's
replay (#35).

## What you need

- Docker with compose v2 (`docker compose version`). `podman compose` works
  too: run the targets as `COMPOSE='podman compose' make up`.
- `make` and `curl`.
- Ports 4222, 8222 and 8080 free on 127.0.0.1.

## The commands

Run them from the root of `paddock`. Each prints the commands it runs.

| Command | What it does |
|---|---|
| `make up` | Starts `nats`, runs the bootstrap once it is healthy, starts `logbook` once the bootstrap has exited 0, then waits up to 60 s for `logbook`'s `/healthz` to answer 200 |
| `make sub` | Prints every message on `twr.>` through `nats-box`, until Ctrl-C |
| `make export` | Stops `logbook`, prints its stored laps (`logbook export --data /data --laps`) as JSON lines, and starts it again |
| `make down` | Stops and removes the containers; the volumes, and with them the streams and `logbook`'s data, stay |
| `make reset` | `down`, and removes the volumes: the next `make up` starts empty |

`make replay FILE=… [SPEED=…]` is added with ecu's replay (#35).

## The services

| Service | Image | What |
|---|---|---|
| `nats` | `nats:2.15.1-alpine` | `-js -sd /data -m 8222`, JetStream on the `nats-data` volume; healthy once `/healthz?js-enabled-only=true` answers |
| `nats-bootstrap` | `natsio/nats-box:0.20.0` | [`bootstrap.sh`](../nats/bootstrap.sh) with `deploy/nats` mounted read-only, as user 1000 on a read-only root file system, as the cluster's Job runs it (#43) |
| `logbook` | `ghcr.io/those-who-run/twr-logbook:0.1.2` | `logbook run`, its store on the `logbook-data` volume at `/data` |
| `nats-box` | `natsio/nats-box:0.20.0` | Profile `tools`: only `make sub` or `docker compose run` starts it |

Every image is pinned by version tag and digest, never `latest`. Renovate's
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
published meanwhile wait in the streams. It prints the laps of source `live`;
for another source, run the export by hand:

```sh
docker compose -f deploy/compose/compose.yaml stop logbook
docker compose -f deploy/compose/compose.yaml run --rm --no-deps logbook \
  export --data /data --laps --src replay-<run>
docker compose -f deploy/compose/compose.yaml start logbook
```

## The same as the cluster

What the cluster also needs is the same here, so the kustomize base copies it
rather than reinventing it: the definitions in `deploy/nats` and the script
that applies them, the bootstrap's user and read-only root file system, the
environment variable names (`NATS_URL`, `TWR_NATS_URL`, `TWR_DATA_DIR`,
`TWR_HEALTH_ADDR`) and `logbook`'s data at `/data`.

## Checks

- `tests/test_compose.py` (in `make test`) reads `compose.yaml`: every image is
  pinned by tag and digest and none is `latest`, every port is published on
  127.0.0.1 only, and the NATS images match `scripts/nats_it.sh`'s.
- `make compose-it` ([`scripts/compose_it.sh`](../../scripts/compose_it.sh),
  in `make check` and CI's `test` job) starts from `make reset`, runs
  `make up`, checks NATS's `/healthz` with JetStream, that the bootstrap made
  `TELEMETRY`, `EVENTS` and the `live` bucket, and that `logbook` reports
  connected, runs `make export` against the running stack, then `make reset`,
  and checks that no container or volume of the `twr` project is left. Replaying
  `real-race-2laps-2026-10-14` and checking that `make export` prints its laps
  come with ecu's replay (#35).
