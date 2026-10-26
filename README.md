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

The roadmap is [`ROADMAP.md`](ROADMAP.md), the issue format is
[`docs/ISSUE_FORMAT.md`](docs/ISSUE_FORMAT.md), and the decisions are in
[`docs/adr`](docs/adr/README.md).

Work is tracked on the
[Race Control](https://github.com/orgs/those-who-run/projects/2) board, which
every `paddock` issue joins on its own; [`docs/board.md`](docs/board.md) says
what its views and statuses mean. The milestone order is in
[`ROADMAP.md`](ROADMAP.md), not on the board.
