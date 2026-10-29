# Deploying v2

v2's cluster manifests, as a base that any cluster can use. The base names
nothing that belongs to one cluster
([ADR 0004](../docs/adr/0004-beside-v1.md), point 8): an overlay outside this
repository sets that.

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
