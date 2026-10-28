# Deploying v2

v2's cluster manifests, as a kustomize base that any cluster can use and
components an overlay switches on. The base names nothing that belongs to one
cluster: no namespace, host, storage class, node or host port
([ADR 0004](../docs/adr/0004-beside-v1.md), point 8). An overlay outside this
repository sets those. The runbook, from nothing to running, comes with #51.

| Directory | What |
|---|---|
| [`base/`](base/kustomization.yaml) | The base (#44): NATS, the stream and KV bootstrap Job, and `logbook` |
| [`nats/`](nats/README.md) | NATS from its Helm chart (`nats/chart`, #42), the streams and buckets, and the bootstrap Job (#43) |
| [`components/nats-websocket/`](components/nats-websocket/kustomization.yaml) | The Ingress to NATS's WebSocket, with the overlay's host, class and TLS secret |
| [`components/network-policy/`](components/network-policy/kustomization.yaml) | NetworkPolicies: only the namespace's pods reach NATS and `logbook`, and the ingress controller's namespace reaches the WebSocket |
| [`compose/`](compose/README.md) | The same stack on a laptop, with no cluster |

## The base

[`base/kustomization.yaml`](base/kustomization.yaml) includes `nats/chart` and
`nats`, and `logbook`:
[`base/logbook-statefulset.yaml`](base/logbook-statefulset.yaml) and its
headless Service. Every object carries `app.kubernetes.io/part-of: twr`.

- **`logbook`** is a StatefulSet of exactly one replica, `OrderedReady`: its
  store is one file that one process writes. Its volume, `data`, is mounted at
  `/data` and is kept when the StatefulSet is deleted or scaled to 0
  (`persistentVolumeClaimRetentionPolicy: Retain`), so the next pod opens the
  same store. The base asks for 1 GiB with no storage class; the overlay sets
  both before the first apply, since a claim template cannot change after it.
  It reaches NATS at `nats://nats:4222`, and its readiness and liveness are on
  `/healthz`, port 8080, which answers 200 once it is connected and its
  consumers are bound. It runs as user 65532 on a read-only root file system
  with no service account token. The image is amd64 only (ADR 0006).
- **Images** are pinned by tag and digest, the same strings as compose:
  `logbook` in the base's `images:` block (0.1.3), NATS, `nats-box` and the
  exporter in `nats/chart/values.yaml` and `nats/bootstrap-job.yaml`. Each is
  pinned in one place. `make deploy-render` fails when an image in the base's
  render is not compose's, or compose runs one the base does not.
- **Components** are opt-in. An overlay that publishes the WebSocket includes
  `nats-websocket` and always gives it a host
  ([`nats/README.md`](nats/README.md#nats-on-a-cluster) has the patch). A
  cluster that enforces NetworkPolicy (k3s does) includes `network-policy` and
  replaces its placeholder `my-ingress-namespace` with the ingress
  controller's namespace (the component's
  [`kustomization.yaml`](components/network-policy/kustomization.yaml) has the
  patch).

Render and apply with kustomize 5.7.x, not 5.8
([kubernetes-sigs/kustomize#6058](https://github.com/kubernetes-sigs/kustomize/issues/6058)),
and `helm` on the `PATH`. `kubectl apply -k` cannot inflate NATS's chart:

```sh
kustomize build --enable-helm <overlay> | kubectl apply -f -
```

Argo CD needs `kustomize.buildOptions: --enable-helm`, and its Application
sets `spec.destination.namespace` explicitly.

## Checks

`make deploy-render` (in `make test`) builds the base and each component over
it with the kustomize, helm and kubeconform that
[`scripts/deploy_render.sh`](../scripts/deploy_render.sh) pins, validates the
output with kubeconform in strict mode, runs
[`scripts/deploy_check.py`](../scripts/deploy_check.py) on it, and builds each
again through an overlay with `namespace: probe` to check every object takes
it. For the base, the check adds the label, the images against compose, NATS at
one replica, and `logbook`'s StatefulSet: exactly one replica, the retention
policy, no storage class, its volume, environment, probes, security settings
and resources.
