# Paddock

The architecture repository for Those Who Run v2: the issues, milestones,
roadmap and decisions (ADRs) for every v2 component, and the deployment base.
It holds no service code; each component has its own repository.

v1, the current release, lives in
[those-who-run/those-who-run](https://github.com/those-who-run/those-who-run)
and is in maintenance from 1.0.0.

The deployment lives under `deploy/`. The JetStream streams and KV buckets,
and the script that creates them, are in
[`deploy/nats/`](deploy/nats/README.md).

## Running the walking skeleton on a laptop

[`deploy/compose/`](deploy/compose/README.md) runs NATS with JetStream, the
stream and KV bootstrap and `logbook` on one machine, with no PS5 and no
cluster. It needs Docker with compose v2, `make` and `curl`. From the root of
this repository:

```sh
make up       # nats, the bootstrap, logbook; waits for logbook's /healthz
make sub      # every message on twr.>, through nats-box, until Ctrl-C
make export   # logbook's stored laps as JSON lines (logbook export --laps)
make down     # stop; the streams and logbook's data stay
make reset    # stop and remove the volumes
```

`logbook`'s data is the `twr_logbook-data` volume, mounted at `/data`.
`logbook export` refuses while `logbook` runs, so `make export` stops it,
reads the volume and starts it again. Every port is on 127.0.0.1 only: 4222
(NATS), 8222 (NATS monitoring) and 8080 (`logbook`'s `/healthz`).
[`deploy/compose/README.md`](deploy/compose/README.md) has the services, the
pinned images and the checks. Replaying a recording (`make replay`) comes with
`ecu`'s replay (#35).

## Comparing v1 and v2

[`parity/`](parity/README.md) is the parity harness (#40): `parity laps`
replays a recording through v1's image and through the compose stack, and
compares the laps each stored. The recordings it and the stack replay are
copied from v1 into [`recordings/`](recordings/README.md), each with its
provenance in `SOURCES.yaml`.

The roadmap is [`ROADMAP.md`](ROADMAP.md), the issue format is
[`docs/ISSUE_FORMAT.md`](docs/ISSUE_FORMAT.md), and the decisions are in
[`docs/adr`](docs/adr/README.md).

Work is tracked on the
[Race Control](https://github.com/orgs/those-who-run/projects/2) board, which
every `paddock` issue joins on its own; [`docs/board.md`](docs/board.md) says
what its views and statuses mean. The milestone order is in
[`ROADMAP.md`](ROADMAP.md), not on the board.
