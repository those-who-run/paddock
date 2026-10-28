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
  docker.io/natsio/nats-box:0.20.0@sha256:d2f04223f40e016f77922a8115d34b0cd5fad2ec10b19b39316ae92d07f3d295 \
  sh /nats/bootstrap.sh /nats
```

It needs only the `nats` CLI and `jq`, both in `natsio/nats-box`, and is POSIX
`sh`, since that image has no `bash`. It takes the server from `NATS_URL`
(required) and the credentials from `NATS_CREDS`, or `NATS_USER` and
`NATS_PASSWORD`, when they are set; nothing in the files is particular to one
installation. Pass credentials through those variables, not in `NATS_URL`. A
user and password written into the URL also work, and the script's own lines
leave them out, but the `nats` CLI's error for a URL it cannot parse may print
the URL whole. It writes no files, so it runs as a non-root user on
a read-only root file system.

For each stream it compares the fields its file names with `nats stream info`;
for each bucket, the same through the bucket's stream, `KV_<bucket>`. It prints
one line for each:

- `created`: it was missing (`nats stream add --config`, `nats kv add`);
- `updated`: a field differed, named on the line before
  (`nats stream edit --config --force`, `nats kv edit`);
- `unchanged`: nothing differed, and nothing was sent.

A second run in a row prints only `unchanged` and exits 0. It never deletes or
purges anything. A definition the server refuses (subjects that overlap another
stream, a change of storage or retention that cannot be made in place) stops the
run with the server's message and a non-zero exit. That stream or bucket stays
as it was, but the run is not all or nothing: the files before it (streams, then
buckets, each in name order) have already been applied, and the ones after it
have not. Once the file is fixed, a rerun applies the rest.
A lower limit is applied by the server as it would be to any stream: the
messages beyond it are discarded.

### On a cluster

[`kustomization.yaml`](kustomization.yaml) runs the script as the Job
`twr-nats-bootstrap` ([`bootstrap-job.yaml`](bootstrap-job.yaml), #43), in the
same `nats-box` image as compose, with the same files. The base (#44) includes
this directory; it also renders on its own with `kustomize build deploy/nats`.

- It connects to `NATS_URL`, `nats://nats:4222` by default: the `nats` Service of
  the chart in `deploy/nats/chart/` (#42), in the same namespace.
  An overlay may patch it. Credentials, if NATS ever has any, come from the
  optional Secret `twr-nats-bootstrap` (`NATS_USER` and `NATS_PASSWORD`); #42 sets
  none.
- It waits up to 5 min (`WAIT_SECONDS`) for the server to answer with JetStream,
  then runs the script. At the deadline it fails with the CLI's own error, and a
  refused definition fails with the server's message, as above.
- It runs as user 1000 on a read-only root file system, with every capability
  dropped and no service account token: it needs no Kubernetes API.
- **It runs again** on every Argo CD sync: it is a `Sync` hook with
  `hook-delete-policy: BeforeHookCreation`. Without Argo CD, apply with
  `kustomize build --enable-helm <dir> | kubectl apply -f -`, since `kubectl
  apply -k` cannot inflate NATS's chart, which the base includes. The Job
  deletes itself 10 min after it finishes (`ttlSecondsAfterFinished: 600`), so
  the next apply creates it again. An apply within 10 min of the last one needs
  `kubectl delete job twr-nats-bootstrap` first, since a Job's template cannot be
  changed in place. A rerun with nothing changed prints only `unchanged`.
- The generated ConfigMaps' names carry a hash of their contents, so a changed
  definition changes the Job's spec.
- An Argo CD Application sets `spec.destination.namespace` explicitly. kustomize
  5.8 leaves the chart's objects out of an overlay's `namespace:`
  ([kubernetes-sigs/kustomize#6058](https://github.com/kubernetes-sigs/kustomize/issues/6058)),
  and they then go to the destination namespace, which must be the Job's.

A new definition file has to be added to its directory's `configMapGenerator`
in [`kustomization.yaml`](kustomization.yaml), since kustomize takes no globs.
`tests/test_bootstrap_job.py` (in `make test`) fails when a file in `streams/` or
`kv/` is not there, and checks the Job's image is compose's, its user, file
system and token, its URL and wait, and the rerun annotations.

For the cluster's Job (#43): a ConfigMap has no directories, so the files go in
two ConfigMaps, one for `streams/` and one for `kv/`, mounted at `streams/` and
`kv/` under the directory given to the script. A new definition file is then
mounted with the others. With a single ConfigMap, the volume's `items` would
have to list every file, and a file left out of that list would never be
mounted, so the Job would pass without creating it.

## Checks

- `make nats` (in `make test`, and a commit hook) reads the files, offline: every
  stream subject covers a subject in `subjects.yaml`, every subject there is
  captured by the stream its entry names and no other, no two stream subjects
  overlap, every stream and bucket sets its limits, every field the bootstrap
  compares is named, and every value is written as the server returns it. A
  stream file that overlaps another fails here, before it reaches a server.
- `make nats-it` starts an empty `nats-server -js` in a container and runs
  [`test/integration.sh`](test/integration.sh) in `nats-box`: the bootstrap
  creates both streams and the bucket with the settings of #36, a second run
  changes nothing, a core publish on `twr.v1.parity.frame.gt7` lands in
  `TELEMETRY` and one on `…frame.gt7.lo` in no stream, an event lands in
  `EVENTS` and is stored once when published twice with one `Nats-Msg-Id`, and
  an overlapping stream ([`test/refused/`](test/refused/streams/overlap.json))
  is refused with the server's message. Then a copy with one limit changed in
  each file is updated in place, and the next run is `unchanged`. A change of
  the bucket's storage is refused. A user and password in `NATS_URL` never
  appear in the output. It needs docker or podman (`CONTAINER` picks one).

Both run in CI's `test` job. The images are pinned by digest in
[`scripts/nats_it.sh`](../../scripts/nats_it.sh).

## Changing a definition

- A new stream or bucket is a new file named after it (`streams/callouts.json`
  is `CALLOUTS`), with every limit set and a `$comment` with the working, and a
  line in its directory's `configMapGenerator` in
  [`kustomization.yaml`](kustomization.yaml), so the cluster's Job mounts it.
- When `contracts` adds subjects, copy its `subjects.yaml` from the new tag over
  [`subjects.yaml`](subjects.yaml), and change `CONTRACTS_TAG` and
  `SUBJECTS_SHA256` in [`scripts/nats_check.py`](../../scripts/nats_check.py).
- Durations in the stream files are nanoseconds and sizes are bytes, as the
  `nats` CLI writes them (`nats stream add --output`).
- Write each value as `nats stream info --json` returns it: `-1`, not `0`, for
  no limit. The server stores `0` as `-1`, so a `0` would differ on every run
  and the stream would be edited every time. `make nats` refuses it.
- Never delete a field to remove a setting; set it to its default. The
  bootstrap compares only the fields a file names, so a deleted field would keep
  its old value on the server while every run said `unchanged`. `make nats`
  fails a stream or bucket file that leaves out any field the bootstrap
  compares.
- The pinned images are bumped by Renovate, through the custom manager in
  [`renovate.json`](../../renovate.json), in `scripts/nats_it.sh` and in the
  example above, and through the docker-compose manager in
  [`deploy/compose/compose.yaml`](../compose/compose.yaml), which runs the same
  images (`tests/test_compose.py` fails when they differ).
