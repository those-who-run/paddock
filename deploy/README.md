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
  `logbook` in the base's `images:` block (0.1.4), NATS, `nats-box` and the
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
  patch). With it on, nothing outside the namespace reaches NATS's Prometheus
  exporter on 7777, so a Prometheus in its own namespace stops scraping. An
  overlay that scrapes it adds a rule for that namespace:

  ```yaml
  patches:
    - target: {kind: NetworkPolicy, name: nats}
      patch: |
        - op: add
          path: /spec/ingress/-
          value:
            from:
              - namespaceSelector:
                  matchLabels:
                    kubernetes.io/metadata.name: <the monitoring namespace>
            ports:
              - {port: 7777, protocol: TCP}
  ```

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
it. For the base, the check adds the label, resource requests on every
container, the images against compose, NATS at one replica, and `logbook`'s
StatefulSet: exactly one replica, the retention policy, no storage class, its
volume, environment, probes, security settings and resources. Each component's
render carries the label too. For `network-policy`, both policies are Ingress
only and select NATS's server and `logbook`, each lets in the namespace's own
pods, and NATS's lets in the ingress controller's namespace, and no other, on
8080 only.

`make check-deploy` (also in `make test`) runs `make deploy-render`, then
[`scripts/private_scan.py`](../scripts/private_scan.py) over every file in
the repository: no private address, host name or cluster context, and no
stream or bucket created outside the bootstrap. The
[portable-base rules](#portable-base-rules) below list what both check, and
why.

## Portable-base rules

paddock is public, and one base serves every cluster, so these rules hold for
every file in the repository. `make check-deploy`, which `make test` and so
CI's required `test` job run, checks them. Each failure names the rule, the
file and the line or object.

In what the base and each component render to
([`scripts/deploy_check.py`](../scripts/deploy_check.py), through
`make deploy-render`):

| Rule | Why |
|---|---|
| No `metadata.namespace` | The overlay sets the namespace, so the same base can run in any namespace, beside v1 in its own. |
| No `storageClassName` | Storage classes differ from cluster to cluster; the overlay sets one, or the cluster's default applies. |
| No `nodeName` or `nodeSelector` | Node names and labels belong to one cluster. |
| No `hostNetwork` or `hostPort` | A host's network and ports belong to one machine, and two copies on one node would collide. |
| No NodePort or LoadBalancer Service | What reaches outside the cluster is the overlay's choice (an Ingress in a component, with the overlay's host). |
| Every image pinned by tag and digest | A render gives the same images on every cluster and every day, and an upgrade is a reviewed change. |
| NATS at exactly one replica | NATS runs as one server with no clustering, and the stream definitions ask for one replica. |
| `logbook` at exactly one replica | Its store is one file that one process writes. |
| Every object labelled `app.kubernetes.io/part-of: twr`, and resource requests on every container | An overlay and a cluster's tools can find the stack's objects, and the scheduler can place its pods. |

A Namespace object, `hostPath` volumes, node affinity and Secret objects are
rules for the base as well, but nothing checks them yet.

In every file git knows, Markdown, docs and ADRs included
([`scripts/private_scan.py`](../scripts/private_scan.py)):

| Rule | Why |
|---|---|
| `address`: no IPv4 address outside `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` and loopback (`0.0.0.0` too, which is every interface) | The repository is public, and an address names someone's machine. The three documentation ranges never route anywhere, so examples use `192.0.2.x`. |
| `host`: no host name except placeholders (`example.com`, `*.example`, `cluster.local` for in-cluster names, and the other names RFC 2606 and 6761 reserve; not `.local`, which is mDNS, a home network's real names), names with no dot (an in-cluster Service, `localhost`), and the public hosts in [`scripts/private_scan.allow`](../scripts/private_scan.allow) | A host name points at someone's setup just as an address does. A public host the docs need (a registry, a specification) goes in the allowlist with the reason it is there. |
| `context`: no cluster context after `--context`, `use-context` or `current-context:` but a placeholder (`my-cluster`, `<context>`, a template) | A context names one cluster and the machine that reaches it. |
| `bootstrap`: no command that adds, edits, copies or restores a JetStream stream, KV bucket or object store with the `nats` CLI, and no request to `$JS.API.STREAM.CREATE` or `UPDATE`, anywhere but [`nats/bootstrap.sh`](nats/bootstrap.sh) | The streams and buckets are declared once, in [`nats/`](nats/README.md), and created only by the bootstrap. A second place that creates them drifts from the files and from what compose and the cluster run. |

The `host` rule reads the host of every URL and after every `user@`, whatever
its top-level domain, and every other dotted name that
ends in a common top-level domain or a home network's suffix (`.com`, `.io`,
`.dev`, `.lan`, `.local`, `.lab`, `.arpa` and the others in the script),
wildcards (`*.`) and a leading dot (a `NO_PROXY` entry) included. File names
such as `compose.yaml` are not hosts to it. The `bootstrap` rule reads commands
only: shell, Makefiles, YAML, Python argument lists and Markdown code blocks,
not comments or prose, so a page can still say what the bootstrap runs. A
command continued with a backslash, or written as a YAML list (a Job's
`args:`), counts as one.

Every file is read whatever its encoding: a NUL byte or bytes that are not UTF-8
don't hide the rest. A symlink is read as git stores it, as the path it points
to. Two kinds of file are not read, and the scan's output lists each one it
leaves out:

- images, fonts, PDFs and archives, by extension: they hold no text to read;
- a Go module's own files: `go.mod`, a `go.sum` beside a `go.mod`, `go.work`,
  and a `go.work.sum` beside a `go.work`. They list module paths, not hosts a
  deployment reaches, and `go` writes them. A `go.sum` anywhere else is read.

Known false positives: a four-part version written without its `v` is read as
an address, and so is a private range in a generic NetworkPolicy's `except:`
list. Write the version with its `v`; a base has no reason to list private
ranges, so a NetworkPolicy that needs one belongs in the overlay.

When the scan fails:

- on a private value, replace it with a placeholder (`nats.example.com`,
  `192.0.2.10`, `my-cluster`). Never allowlist it: it belongs in the private
  overlay. The ADRs are records; if one ever holds a private value, tell the
  maintainer rather than editing or allowlisting it;
- on a public host that a doc or a manifest needs, add it to
  `scripts/private_scan.allow` with its reason. An entry allows the host and
  the names under it, so add the narrowest one. The allowlist refuses a name
  under a home network's suffix (`.lan`, `.local`, `.internal` and the others
  in the script). An entry no file uses fails
  the scan, so remove it with the last use.
