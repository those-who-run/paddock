# NATS streams and KV buckets

The JetStream streams and KV buckets v2 needs, as files, and the script that
creates or updates them (#36). Compose (#39) and the cluster's bootstrap Job
(#43) run the same script with the same files, so a setting cannot drift
between the laptop and the cluster. The streams and buckets are
[ADR 0001](../../docs/adr/0001-v2-architecture.md), point 1.

| File | What | Settings |
|---|---|---|
| [`streams/telemetry.json`](streams/telemetry.json) | `TELEMETRY`: full-rate frames, `twr.v1.*.frame.gt7` (not the 20 Hz `.lo` copy) | file, limits, 2 h, 2 GiB, discard old, 2 min duplicate window, 1 replica |
| [`streams/events.json`](streams/events.json) | `EVENTS`: domain events, `twr.v1.*.evt.>` | file, limits, 30 d, 1 GiB, discard old, 2 min duplicate window, 1 replica |
| [`kv/live.json`](kv/live.json) | `live`: the current state, every key carrying its source | file, history 1, 64 KiB a value, 64 MiB, 1 replica |
| [`subjects.yaml`](subjects.yaml) | A copy of `contracts`' `subjects.yaml` at `v0.1.3`, for the check below | |
| [`bootstrap.sh`](bootstrap.sh) | Creates what is missing, updates what differs | |

Each file's `$comment` says why each value is what it is. The limits are a
buffer for subscribers, not how long results are kept: `logbook`'s store keeps
sessions, laps and frames after the streams have aged them out. `CALLOUTS` and
the `config` bucket are added by the milestones that first use them.

## Running the bootstrap

```sh
docker run --rm --network host -v "$PWD/deploy/nats:/nats:ro" \
  -e NATS_URL=nats://localhost:4222 \
  natsio/nats-box:0.20.0 sh /nats/bootstrap.sh /nats
```

It needs only the `nats` CLI and `jq`, both in `natsio/nats-box`, and is POSIX
`sh`, since that image has no `bash`. It takes the server from `NATS_URL`
(required) and the credentials from `NATS_CREDS`, or `NATS_USER` and
`NATS_PASSWORD`, when they are set; nothing in the files is particular to one
installation. It writes no files, so it runs as a non-root user on a read-only
root file system.

For each stream it compares the fields its file names with `nats stream info`;
for each bucket, the same through the bucket's stream, `KV_<bucket>`. It prints
one line for each:

- `created`: it was missing (`nats stream add --config`, `nats kv add`);
- `updated`: a field differed, named on the line before
  (`nats stream edit --config --force`, `nats kv edit`);
- `unchanged`: nothing differed, and nothing was sent.

A second run in a row prints only `unchanged` and exits 0. It never deletes or
purges anything. A definition the server refuses (subjects that overlap another
stream, a change of storage or retention that cannot be made in place) stops it
with the server's message and a non-zero exit, and the stream stays as it was.
A lower limit is applied by the server as it would be to any stream: the
messages beyond it are discarded.

For the cluster's Job (#43): a ConfigMap has no directories, so the files go in
as keys (`telemetry.json`, `events.json`, `live.json`) and the volume's `items`
put them back at `streams/` and `kv/` under the directory given to the script.

## Checks

- `make nats` (in `make test`, and a commit hook) reads the files, offline: every
  stream subject covers a subject in `subjects.yaml`, every subject there is
  captured by the stream its entry names and no other, no two stream subjects
  overlap, and every stream and bucket sets its limits. A stream file that
  overlaps another fails here, before it reaches a server.
- `make nats-it` starts an empty `nats-server -js` in a container and runs
  [`test/integration.sh`](test/integration.sh) in `nats-box`: the bootstrap
  creates both streams and the bucket with the settings of #36, a second run
  changes nothing, a core publish on `twr.v1.parity.frame.gt7` lands in
  `TELEMETRY` and one on `…frame.gt7.lo` in no stream, an event lands in
  `EVENTS` and is stored once when published twice with one `Nats-Msg-Id`, and
  an overlapping stream ([`test/refused/`](test/refused/streams/overlap.json))
  is refused with the server's message. It needs docker or podman
  (`CONTAINER` picks one).

Both run in CI's `test` job. The images are pinned by digest in
[`scripts/nats_it.sh`](../../scripts/nats_it.sh).

## Changing a definition

- A new stream or bucket is a new file named after it (`streams/callouts.json`
  is `CALLOUTS`), with every limit set and a `$comment` with the working.
- When `contracts` adds subjects, copy its `subjects.yaml` from the new tag over
  [`subjects.yaml`](subjects.yaml), and change `CONTRACTS_TAG` and
  `SUBJECTS_SHA256` in [`scripts/nats_check.py`](../../scripts/nats_check.py).
- Durations in the stream files are nanoseconds and sizes are bytes, as the
  `nats` CLI writes them (`nats stream add --output`).
