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
| [`streams/callouts.json`](streams/callouts.json) | `CALLOUTS`: what `ecu`'s rules decide to say, `twr.v1.*.callout.>` (`p0`, `p1`, `p2`), for `radio` to speak once (#73) | file, work queue, 30 s, 64 MiB, discard old, 30 s duplicate window on the callout's id, 1 replica |
| [`kv/live.json`](kv/live.json) | `live`: the current state, every key carrying its source: `ecu.status`, `ecu.session`, `ecu.pit` and `ecu.fuel` of each source | file, history 1, 64 KiB a value, 64 MiB, 1 replica |
| [`kv/ecu.json`](kv/ecu.json) | `ecu`: `ecu`'s own state, which no reader of `live` sees: `sessions.open.<src>`, the open session a restarted `ecu` continues or ends (#73, #76) | file, history 1, 1 MiB a value, 64 MiB, 1 replica |
| [`subjects.yaml`](subjects.yaml) | A copy of `contracts`' `subjects.yaml` at `v0.2.3`, for the check below | |
| [`bootstrap.sh`](bootstrap.sh) | Creates what is missing, updates what differs | |

Each file's `$comment` says why each value is what it is. The limits are a
buffer for subscribers, not how long results are kept: `logbook`'s store keeps
sessions, laps and frames after the streams have aged them out. The keys in
each bucket, and what each value is, are `contracts`' (its `subjects.yaml`).
`ecu` alone writes them; the bootstrap only creates the buckets. The `config`
bucket is added by the milestone that first uses it.

`CALLOUTS` keeps a callout 30 s, not ADR 0001's 15 s, as decided on #73: the
race summary's own expiry is 30 s, so the stream never ages out a callout a rule
still allows. Its duplicate window is 30 s too, not #73's 2 min: the server cuts
a window longer than `max_age` to `max_age`, and `make nats` refuses one that is.

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
  the next apply creates it again. Within those 10 min, an apply with nothing
  changed leaves the finished Job as it is and does not run it again, which is
  harmless. An apply with a changed definition changes the Job's template (below),
  which cannot be changed in place, and fails with `field is immutable`: run
  `kubectl delete job twr-nats-bootstrap` first. Without Argo CD, a failed Job's
  log also goes with it after 10 min, so read it (`kubectl logs
  job/twr-nats-bootstrap`) before then. A rerun with nothing changed prints only
  `unchanged`.
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

## NATS on a cluster

[`chart/`](chart/kustomization.yaml) is NATS for the cluster (#42), from the
official `nats` chart of [nats-io/k8s](https://github.com/nats-io/k8s/tree/main/helm/charts/nats)
at 2.15.1, the server version compose runs. kustomize inflates it, so it renders
with one command and no Argo CD:

```sh
kustomize build --enable-helm deploy/nats/chart
```

kustomize runs `helm` to fetch the chart, so `helm` must be on the `PATH`.
`kubectl apply -k` cannot render it, as `kubectl`'s built-in kustomize has no
`--enable-helm`. Without Argo CD, apply the build instead:

```sh
kustomize build --enable-helm <dir> | kubectl apply -f -
```

Use kustomize 5.7.x or earlier, not 5.8: from 5.8.0 an overlay's `namespace:`
is not applied to what `helmCharts` renders
([kubernetes-sigs/kustomize#6058](https://github.com/kubernetes-sigs/kustomize/issues/6058)).
Only the objects that are not the chart's (the WebSocket's Ingress) land in the
overlay's namespace; NATS's ConfigMap, Services, StatefulSet and `nats-box` land
in whatever namespace the apply defaults to. 5.4.3
to 5.7.1 namespace every object. `make deploy-render` pins 5.7.1 and builds each
kustomization and component through an overlay with `namespace: probe`, failing if
any object comes out without it, so a later kustomize cannot bring the bug back unnoticed.

Argo CD needs `kustomize.buildOptions: --enable-helm` in `argocd-cm`. Its
Application must also set `spec.destination.namespace` explicitly: with a
kustomize 5.8 in Argo CD, the objects the overlay's `namespace:` misses go there
instead of to Argo CD's default.

Only what differs from the chart's defaults is in
[`chart/values.yaml`](chart/values.yaml), each with the reason:

- one server, no clustering; JetStream on file storage at `/data` on a 5 GiB
  volume (`nats-js`), memory storage off;
- the client port 4222 and the WebSocket's 8080 on the `nats` Service, a ClusterIP,
  so a component in the namespace uses `nats://nats:4222`; monitoring 8222 only on
  the headless Service;
- the WebSocket with no TLS in the cluster. Nothing outside the cluster reaches it
  until an overlay includes the component
  [`deploy/components/nats-websocket`](../components/nats-websocket/kustomization.yaml),
  the Ingress `nats-ws`, where TLS ends;
- no authentication in the cluster, as decided on #42: the namespace is the
  boundary. A client with no credentials is `twr` on 4222, and may do anything, and
  `ws` on the WebSocket, and may only subscribe to `twr.>`. Not to `_INBOX.>`,
  which #42 R6 names: `ws` cannot publish, so it makes no request that needs a
  reply inbox, and `_INBOX.>` would let it read the replies to requests made in the
  cluster. Each user is allowed only its own listener, so a WebSocket client cannot
  name itself `twr`;
- no service account token and the runtime's default seccomp profile for the
  server's and `nats-box`'s pods, and a memory limit on every container;
- the Prometheus exporter beside the server on 7777, and the `nats-box` Deployment:
  `kubectl exec deploy/nats-box -- nats sub 'twr.>'`;
- every image pinned by tag and digest, the same as compose's; no config reloader,
  so a change to the config restarts the server through the checksum annotation.

The base names no namespace, host, ingress class, TLS secret or storage class (ADR
0004, point 8). An overlay that publishes the WebSocket includes the component and
patches its Ingress; every overlay sets the volume. These set the example overlay's
placeholders. The volume's size and the server's file store limit,
`NATS_JS_MAX_FILE_STORE`, are set together, since the config cannot read the
volume's size:

```yaml
components:
  - <path to paddock>/deploy/components/nats-websocket
patches:
  - target: {kind: Ingress, name: nats-ws}
    patch: |
      - {op: add, path: /spec/ingressClassName, value: my-ingress-class}
      - {op: add, path: /spec/rules/0/host, value: nats.example.com}
      - op: add
        path: /spec/tls
        value: [{hosts: [nats.example.com], secretName: twr-nats-tls}]
  - target: {kind: StatefulSet, name: nats}
    patch: |
      - op: add
        path: /spec/volumeClaimTemplates/0/spec/storageClassName
        value: my-storage-class
      - op: replace
        path: /spec/volumeClaimTemplates/0/spec/resources/requests/storage
        value: 5Gi
  - patch: |
      apiVersion: apps/v1
      kind: StatefulSet
      metadata: {name: nats}
      spec:
        template:
          spec:
            containers:
              - name: nats
                env: [{name: NATS_JS_MAX_FILE_STORE, value: 5Gi}]
```

NATS's volume need not survive removing the stack: the streams are buffers, and
`logbook`'s volume is the one that keeps results (#42).

## Checks

- `make nats` (in `make test`, and a commit hook) reads the files, offline: every
  stream subject covers a subject in `subjects.yaml`, every subject there is
  captured by the stream its entry names and no other, no two stream subjects
  overlap, every stream and bucket sets its limits, every field the bootstrap
  compares is named, and every value is written as the server returns it. A
  stream file that overlaps another fails here, before it reaches a server.
- `make nats-it` starts an empty `nats-server -js` in a container and runs
  [`test/integration.sh`](test/integration.sh) in `nats-box`: the bootstrap
  creates the streams and buckets with the settings of #36 and #73, a second run
  changes nothing, a core publish on `twr.v1.parity.frame.gt7` lands in
  `TELEMETRY` and one on `…frame.gt7.lo` in no stream, an event lands in
  `EVENTS` and is stored once when published twice with one `Nats-Msg-Id`, a
  callout on `twr.v1.replay-test.callout.p0` lands in `CALLOUTS` once for one
  `Nats-Msg-Id` and is gone once it is older than 30 s, and
  an overlapping stream ([`test/refused/`](test/refused/streams/overlap.json))
  is refused with the server's message. Then a copy with one limit changed in
  each file is updated in place, and the next run is `unchanged`. A change of
  the bucket's storage is refused. A user and password in `NATS_URL` never
  appear in the output. It needs docker or podman (`CONTAINER` picks one).

- `make deploy-render` (in `make test`) renders [`chart/`](chart/kustomization.yaml)
  with the kustomize, helm and kubeconform that
  [`scripts/deploy_render.sh`](../../scripts/deploy_render.sh) pins, validates it
  with kubeconform in strict mode, and runs
  [`scripts/deploy_check.py`](../../scripts/deploy_check.py) on it: no namespace,
  storage class, node pinning or host port, every image pinned by tag and digest,
  one replica, JetStream on file storage with the volume's size as its limit, only
  4222 and 8080 on the `nats` Service, no Ingress in the base, the pods' token and
  seccomp profile, the exporter and `nats-box`, and the two users above; and,
  rendered over the base, that the component's Ingress has no host, class or TLS
  and goes to the WebSocket.

All three run in CI's `test` job. The images are pinned by digest in
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
  [`renovate.json`](../../renovate.json), in `scripts/nats_it.sh`, in the
  bootstrap example above and in [`chart/values.yaml`](chart/values.yaml), and
  through the docker-compose manager in
  [`deploy/compose/compose.yaml`](../compose/compose.yaml), which runs the same
  images (`tests/test_compose.py` and `tests/test_deploy_check.py` fail when they
  differ). Its kustomize manager bumps the chart's version in
  [`chart/kustomization.yaml`](chart/kustomization.yaml); bump the images with it,
  as the chart's version is the server's.
