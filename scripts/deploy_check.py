"""Check what a kustomization under deploy/ renders to (paddock#42).

scripts/deploy_render.sh renders each kustomization with ``kustomize build
--enable-helm``, validates it with kubeconform, and runs this on the output:

    python3 scripts/deploy_check.py KUSTOMIZATION RENDERED.yaml

For every kustomization, the rules a public base keeps (ADR 0004, point 8): no
namespace, no storage class, no node pinning, nothing on the host's network or
ports, no Service beyond the cluster, and every image pinned by tag and digest.
paddock#50 replaces these with the portable-base check over deploy/base.

For ``deploy/nats/chart``, what paddock#42 asks of NATS:

- R2: one StatefulSet replica and no cluster block; JetStream on, file storage at
  ``/data``, memory storage off (``max_memory_store: 0``); the file store's limit
  read from ``NATS_JS_MAX_FILE_STORE``, which is the volume's size;
- R3: the ``nats`` Service is a ClusterIP with 4222 and the WebSocket's 8080 only;
  monitoring 8222 is on no Service but the headless one; the WebSocket has no TLS;
  and there is no Ingress: the base exposes nothing;
- R4: the exporter beside the server, and the ``nats-box`` Deployment;
- R6: a client with no credentials is ``twr`` on 4222 and ``ws`` on the WebSocket;
  ``twr`` is allowed only standard connections, and ``ws`` only WebSocket ones, may
  publish nothing and may subscribe to ``twr.>`` only;
- the server's and nats-box's pods mount no service account token and run with the
  runtime's default seccomp profile.

For ``deploy/base``, what paddock#44 asks of the base:

- R5: every container has resource requests;
- R1: every object carries ``app.kubernetes.io/part-of: twr``, and the images are
  compose's: every image in the render is in ``deploy/compose/compose.yaml`` (but
  NATS's Prometheus exporter, which compose does not run), and every image compose
  runs is in the render (but ecu's, which compose runs only to replay), so a bump
  to one and not the other fails;
- NATS at one replica, and no Ingress: the base exposes nothing;
- R2: ``logbook`` a StatefulSet of exactly one replica, ``OrderedReady``, its claims
  kept when it is deleted or scaled (``Retain``), its volume at ``/data`` with no
  storage class, ``TWR_NATS_URL=nats://nats:4222`` and ``TWR_HEALTH_ADDR=:8080``,
  readiness and liveness on ``/healthz``, non-root on a read-only root file system
  with no service account token and the RuntimeDefault seccomp profile, requests
  and a memory limit; and its headless Service;
- paddock#63 R2: ``radio`` a Deployment of exactly one replica, replaced by
  ``Recreate``, ``TWR_NATS_URL=nats://nats:4222``, ``TWR_HEALTH_ADDR=:8080``,
  source ``live`` and the ``console`` sink, no Secret and no Discord setting,
  an ``emptyDir`` at ``/tmp`` for the speech cache, readiness and liveness on
  ``/healthz``, non-root on a read-only root file system with no service account
  token and the RuntimeDefault seccomp profile, requests and a memory limit; and
  its Service on 8080;
- paddock#67: ``podium`` a Deployment of exactly one replica, replaced by
  ``Recreate``, ``TWR_NATS_URL=nats://nats:4222``, ``ADDR=:8080``, source ``live``
  and the ``discord`` sink with no Secret and no Discord setting, so its cards are
  off, readiness and liveness on ``/healthz``, non-root on a read-only root file
  system with no service account token and the RuntimeDefault seccomp profile,
  requests and a memory limit; and its Service on 8080.

Each component's render over the base carries the label on every object too, its
own included. For ``deploy/components/network-policy``, rendered over the base: a NetworkPolicy
for NATS's server and one for ``logbook``, each letting in the namespace's own
pods, and NATS's letting the ingress controller's namespace, a placeholder the
overlay replaces, reach the WebSocket's 8080 only.

- paddock#132: ``pitwall`` a Deployment of one replica with no volume,
  ``TWR_NATS_URL=nats://nats:4222``, ``ADDR=:8080`` and the sources ``live`` and
  ``v1-import`` (D7), readiness and liveness on ``/healthz``, non-root on a read-only
  root file system with no service account token and the RuntimeDefault seccomp
  profile, requests and a memory limit; and its Service on 8080.

For ``deploy/components/pitwall-ingress``, rendered over the base: the Ingress
``pitwall`` goes to the ``pitwall`` Service's http port, with no host, class or TLS
of its own, which the overlay that includes it sets.

For ``deploy/examples/ecu-live``, the live engineer an overlay copies (paddock#108):
the rules above but the host port, which is the example's point, and that only for
``Deployment/ecu``'s 33740; every object labelled and requests on every container,
as in the base; and ``ecu`` a Deployment of exactly one replica, replaced by
``Recreate``, with UDP 33740 as a ``hostPort``, ``TWR_NATS_URL=nats://nats:4222``,
``ADDR=:8080``, source ``live``, ``TWR_ECU_PS5_IP`` the placeholder ``192.0.2.10``,
``TWR_ECU_RECORDINGS_DIR`` on a volume, readiness and liveness on ``/healthz``,
non-root on a read-only root file system with no service account token and the
RuntimeDefault seccomp profile, a memory limit, and compose's ecu image.

For ``deploy/components/nats-websocket``, rendered over the base: the Ingress
``nats-ws`` goes to the ``nats`` Service's websocket port, with no host, class or
TLS of its own, which the overlay that includes it sets.

For ``deploy/components/radio-discord``, rendered over the base (paddock#63 R3):
``radio``'s sink is ``discord``, and ``TWR_DISCORD_BOT_TOKEN`` and
``TWR_DISCORD_VOICE_CHANNEL_ID`` come from a ``secretKeyRef`` each, of the Secret
``radio-discord`` under their own names, and from no value; the render holds no
Secret of that name.

For ``deploy/components/podium-discord``, rendered over the base (paddock#67):
``podium``'s ``TWR_DISCORD_BOT_TOKEN`` and ``TWR_DISCORD_RESULTS_CHANNEL_ID`` come
from a ``secretKeyRef`` each, of the Secret ``radio-discord`` under their own names,
the channel's ``optional`` so a Secret without it turns cards off, and from no
value; the render holds no Secret of that name.

With ``--namespace NS``, the render is of a throwaway overlay that sets
``namespace: NS`` over the kustomization, and the only check is that every namespaced
object carries it. kustomize 5.8 leaves helmCharts output out of an overlay's
namespace (kubernetes-sigs/kustomize#6058); this catches that, or any other object
an overlay would miss.

kustomize writes a fixed, block style of YAML, and this reads it line by line,
standard library only as paddock's other checks are. The server's config is the
JSON the chart writes into the ``nats-config`` ConfigMap, read as JSON.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

NATS_CHART = "deploy/nats/chart"
NATS_WEBSOCKET = "deploy/components/nats-websocket"
BASE = "deploy/base"
NETWORK_POLICY = "deploy/components/network-policy"
RADIO_DISCORD = "deploy/components/radio-discord"
# The Secret deploy/components/radio-discord reads, and its keys, which the overlay
# provides.
RADIO_SECRET = "radio-discord"
RADIO_SECRET_KEYS = ("TWR_DISCORD_BOT_TOKEN", "TWR_DISCORD_VOICE_CHANNEL_ID")
PODIUM_DISCORD = "deploy/components/podium-discord"
PITWALL_INGRESS = "deploy/components/pitwall-ingress"
# The keys deploy/components/podium-discord reads from radio's Secret: the bot's
# token, and the results channel, which the overlay adds.
PODIUM_SECRET_KEYS = ("TWR_DISCORD_BOT_TOKEN", "TWR_DISCORD_RESULTS_CHANNEL_ID")
COMPOSE = Path(__file__).resolve().parent.parent / "deploy" / "compose" / "compose.yaml"
# In the cluster beside NATS (deploy/nats/chart, promExporter), with nothing to
# scrape it on a laptop, so compose does not run it.
NOT_IN_COMPOSE = ("docker.io/natsio/prometheus-nats-exporter:",)
# Compose runs ecu only for `ecu replay` (its `replay` service, profile `replay`), and
# replays are local and CI only (paddock#45 is closed), so the base does not run it.
# ecu listening to the PS5 is deploy/examples/ecu-live's, which an overlay copies.
NOT_IN_BASE = ("ghcr.io/those-who-run/twr-ecu:",)
# The live engineer an overlay copies (paddock#108), outside the base: it holds UDP
# 33740 on the node, the one host port any render may have, and only there.
ECU_LIVE = "deploy/examples/ecu-live"
ECU_HOST_PORT = "portable: Deployment/ecu has a host port: hostPort: 33740"
# The PS5's address in the example: a documentation address, never a real one.
PS5_PLACEHOLDER = "192.0.2.10"
# The ingress controller's namespace in deploy/components/network-policy, which the
# overlay replaces.
INGRESS_NAMESPACE = "my-ingress-namespace"

PINNED = re.compile(r"^[a-z0-9.-]+(?::\d+)?/[a-z0-9._/-]+:[A-Za-z0-9._-]+@sha256:[a-f0-9]{64}$")
IMAGE = re.compile(r"^\s*(?:- )?image: (\S+)$")
# Lines a portable base never has, whatever object they are in.
FORBIDDEN = (
    (re.compile(r"^\s*namespace:"), "a namespace, which the overlay sets"),
    (re.compile(r"^\s*storageClassName:"), "a storage class, which the overlay sets"),
    (re.compile(r"^\s*(nodeName|nodeSelector):"), "node pinning"),
    (re.compile(r"^\s*hostNetwork: true$"), "the host's network"),
    (re.compile(r"^\s*(- )?hostPort:"), "a host port"),
    (re.compile(r"^\s*type: (NodePort|LoadBalancer)$"), "a Service beyond the cluster"),
)


class Doc:
    def __init__(self, kind: str, name: str, lines: list[str]) -> None:
        self.kind = kind
        self.name = name
        self.lines = lines

    def __str__(self) -> str:
        return f"{self.kind}/{self.name}"


def documents(text: str) -> list[Doc]:
    """Each object in a `kustomize build` output: its kind, metadata.name and lines."""
    docs: list[Doc] = []
    for chunk in re.split(r"^---$", text, flags=re.M):
        lines = chunk.strip("\n").splitlines()
        if not any(line.strip() for line in lines):
            continue
        kind = next((m.group(1) for line in lines if (m := re.match(r"^kind: (\S+)$", line))), "?")
        name = "?"
        in_metadata = False
        for line in lines:
            if re.match(r"^\S", line):
                in_metadata = line == "metadata:"
            elif in_metadata and (m := re.match(r"^  name: (\S+)$", line)):
                name = m.group(1)
                break
        docs.append(Doc(kind, name, lines))
    return docs


def portable(docs: list[Doc]) -> list[str]:
    problems = []
    for doc in docs:
        for line in doc.lines:
            for pattern, what in FORBIDDEN:
                if pattern.match(line):
                    problems.append(f"portable: {doc} has {what}: {line.strip()}")
            m = IMAGE.match(line)
            if m and not PINNED.match(m.group(1)):
                problems.append(f"pinned: {doc} has an image without a tag and digest: {m.group(1)}")
    return problems


# Kinds that live outside any namespace; every other kind must carry the overlay's.
CLUSTER_SCOPED = {
    "Namespace", "CustomResourceDefinition", "ClusterRole", "ClusterRoleBinding",
    "PersistentVolume", "StorageClass", "IngressClass", "PriorityClass", "RuntimeClass",
    "MutatingWebhookConfiguration", "ValidatingWebhookConfiguration", "APIService",
}


def namespaced(docs: list[Doc], namespace: str) -> list[str]:
    problems = []
    for doc in docs:
        if doc.kind in CLUSTER_SCOPED:
            continue
        found = None
        in_metadata = False
        for line in doc.lines:
            if re.match(r"^\S", line):
                in_metadata = line == "metadata:"
            elif in_metadata and (m := re.match(r"^  namespace: (\S+)$", line)):
                found = m.group(1)
        if found != namespace:
            problems.append(
                f"namespace: {doc} is in {found or 'no namespace'} through an overlay that sets "
                f"{namespace} (kustomize#6058?)"
            )
    return problems


def literal_block(lines: list[str], key: str) -> str | None:
    """The text of a `key: |` block, with its indentation taken off."""
    for i, line in enumerate(lines):
        m = re.match(rf"^(\s*){re.escape(key)}: \|-?$", line)
        if not m:
            continue
        indent = len(m.group(1))
        body = []
        for following in lines[i + 1 :]:
            if following.strip() and len(following) - len(following.lstrip()) <= indent:
                break
            body.append(following)
        depth = min(len(b) - len(b.lstrip()) for b in body if b.strip())
        return "\n".join(b[depth:] for b in body)
    return None


def nats_conf(text: str) -> dict:
    """The chart's nats.conf: JSON but for `$VAR` references, which become strings."""
    return json.loads(re.sub(r'(?<!")\$([A-Za-z_][A-Za-z0-9_]*)', r'"$\1"', text))


def find(docs: list[Doc], kind: str, name: str) -> Doc | None:
    return next((d for d in docs if d.kind == kind and d.name == name), None)


def values(lines: list[str], key: str) -> list[str]:
    return [m.group(1) for line in lines if (m := re.match(rf"^\s*(?:- )?{key}: (.+)$", line))]


def env_value(lines: list[str], name: str) -> str | None:
    for i, line in enumerate(lines):
        if re.match(rf"^\s*- name: {re.escape(name)}$", line) and i + 1 < len(lines):
            m = re.match(r"^\s*value: (.+)$", lines[i + 1])
            if m:
                return m.group(1).strip("'\"")
    return None


def nats_chart(docs: list[Doc]) -> list[str]:
    problems: list[str] = []

    def need(kind: str, name: str) -> Doc | None:
        doc = find(docs, kind, name)
        if doc is None:
            problems.append(f"nats: no {kind}/{name} in the render")
        return doc

    cm = need("ConfigMap", "nats-config")
    sts = need("StatefulSet", "nats")
    svc = need("Service", "nats")
    box = need("Deployment", "nats-box")

    for doc in (sts, box):
        if doc is None:
            continue
        if values(doc.lines, "automountServiceAccountToken") != ["false"]:
            problems.append(f"pod: {doc} mounts a service account token")
        if values(doc.lines, "type") != ["RuntimeDefault"]:
            problems.append(f"pod: {doc} does not run with the RuntimeDefault seccomp profile")

    if cm is not None:
        text = literal_block(cm.lines, "nats.conf")
        if text is None:
            problems.append(f"nats: {cm} has no nats.conf")
        else:
            problems += nats_config(str(cm), nats_conf(text))

    if sts is not None:
        if values(sts.lines, "replicas") != ["1"]:
            problems.append(f"R2: {sts} has replicas {values(sts.lines, 'replicas')}, not 1")
        size = values(sts.lines, "storage")
        limit = env_value(sts.lines, "NATS_JS_MAX_FILE_STORE")
        if len(size) != 1 or limit != size[0]:
            problems.append(
                f"R2: {sts}'s NATS_JS_MAX_FILE_STORE is {limit}, not its volume's size {size}"
            )
        containers = [n for n in values(sts.lines, "name") if n in ("nats", "prom-exporter")]
        if "prom-exporter" not in containers:
            problems.append(f"R4: {sts} has no prom-exporter container")

    if svc is not None:
        ports = sorted(values(svc.lines, "port"))
        if ports != ["4222", "8080"]:
            problems.append(f"R3: {svc} publishes ports {ports}, not 4222 and 8080")
        if any(t != "ClusterIP" for t in values(svc.lines, "type")):
            problems.append(f"R3: {svc} is not a ClusterIP Service")
    for doc in docs:
        if doc.kind == "Service" and doc.name != "nats-headless" and "8222" in values(doc.lines, "port"):
            problems.append(f"R3: {doc} publishes monitoring on 8222")

    for doc in docs:
        if doc.kind == "Ingress":
            problems.append(f"R3: {doc} is in the base, which exposes nothing until an overlay includes {NATS_WEBSOCKET}")
    return problems


def nats_websocket(docs: list[Doc]) -> list[str]:
    ingress = find(docs, "Ingress", "nats-ws")
    if ingress is None:
        return [f"R3: no Ingress/nats-ws through {NATS_WEBSOCKET}"]
    problems = []
    for key in ("host", "ingressClassName", "tls", "secretName"):
        if any(re.match(rf"^\s*(- )?{key}:", line) for line in ingress.lines):
            problems.append(f"R3: {ingress} sets {key}, which the overlay sets")
    if values(ingress.lines, "name")[1:] != ["nats", "websocket"]:
        problems.append(f"R3: {ingress} does not go to the nats Service's websocket port")
    return problems


def images(lines: list[str]) -> list[str]:
    return [m.group(1) for line in lines if (m := IMAGE.match(line))]


def labelled(docs: list[Doc]) -> list[str]:
    return [f"R1: {doc} has no label app.kubernetes.io/part-of: twr"
            for doc in docs if "    app.kubernetes.io/part-of: twr" not in doc.lines]


def containers(lines: list[str]) -> list[list[str]]:
    """Each container's lines, of every `containers:` and `initContainers:` list:
    kustomize writes a list's items at its key's indentation."""
    found: list[list[str]] = []
    indent: int | None = None
    for line in lines:
        depth = len(line) - len(line.lstrip())
        if re.match(r"^\s*(containers|initContainers):$", line):
            indent = depth
            continue
        if indent is None:
            continue
        if depth == indent and line.lstrip().startswith("- "):
            found.append([line])
        elif depth > indent and found:
            found[-1].append(line)
        else:
            indent = None
    return found


def container_name(container: list[str]) -> str:
    """The container's own name, not an env variable's or a port's."""
    first = container[0]
    own = " " * (len(first) - len(first.lstrip()) + 2)
    for line in container:
        m = re.match(rf"^(?:{re.escape(own)}|\s*- )name: (\S+)$", line)
        if m and (line is first or line.startswith(own + "name:")):
            return m.group(1)
    return "?"


def requests(docs: list[Doc]) -> list[str]:
    problems = []
    for doc in docs:
        for container in containers(doc.lines):
            name = container_name(container)
            if not any(line.strip() == "requests:" for line in container):
                problems.append(f"R5: {doc}'s container {name} has no resource requests")
    return problems


def base(docs: list[Doc], compose: str) -> list[str]:
    problems: list[str] = labelled(docs) + requests(docs)
    for doc in docs:
        if doc.kind == "Ingress":
            problems.append(f"R3: {doc} is in the base, which exposes nothing until an overlay includes"
                            f" {NATS_WEBSOCKET} or {PITWALL_INGRESS}")

    rendered = {i for doc in docs for i in images(doc.lines)}
    composed = set(images(compose.splitlines()))
    for image in sorted(rendered - composed):
        if not image.startswith(NOT_IN_COMPOSE):
            problems.append(f"R1: the base runs {image}, which deploy/compose/compose.yaml does not")
    for image in sorted(composed - rendered):
        if image.startswith(NOT_IN_BASE):
            continue
        problems.append(f"R1: deploy/compose/compose.yaml runs {image}, which the base does not")

    nats = find(docs, "StatefulSet", "nats")
    if nats is None:
        problems.append("base: no StatefulSet/nats in the render")
    elif values(nats.lines, "replicas") != ["1"]:
        problems.append(f"replicas: {nats} has replicas {values(nats.lines, 'replicas')}, not 1")
    return problems + logbook(docs) + radio(docs) + podium(docs) + pitwall(docs)


def logbook(docs: list[Doc]) -> list[str]:
    sts = find(docs, "StatefulSet", "logbook")
    if sts is None:
        return ["R2: no StatefulSet/logbook in the render"]
    problems = []
    lines = sts.lines

    def has(key: str, value: str) -> bool:
        return value in values(lines, key)

    if values(lines, "replicas") != ["1"]:
        problems.append(f"replicas: {sts} has replicas {values(lines, 'replicas')}, not exactly 1: one writer")
    if values(lines, "podManagementPolicy") != ["OrderedReady"]:
        problems.append(f"R2: {sts} is not OrderedReady")
    if values(lines, "whenDeleted") != ["Retain"] or values(lines, "whenScaled") != ["Retain"]:
        problems.append(f"retention: {sts} does not keep its claims when deleted and scaled (Retain)")
    if values(lines, "storageClassName"):
        problems.append(f"storage: {sts}'s volume names a storage class, which the overlay sets")
    if not has("mountPath", "/data") or env_value(lines, "TWR_DATA_DIR") != "/data":
        problems.append(f"R2: {sts} has no data volume at /data")
    if env_value(lines, "TWR_NATS_URL") != "nats://nats:4222":
        problems.append(f"R2: {sts}'s TWR_NATS_URL is {env_value(lines, 'TWR_NATS_URL')}, not nats://nats:4222")
    if env_value(lines, "TWR_HEALTH_ADDR") != ":8080":
        problems.append(f"R2: {sts}'s TWR_HEALTH_ADDR is {env_value(lines, 'TWR_HEALTH_ADDR')}, not :8080")
    for probe in ("readinessProbe", "livenessProbe"):
        if not any(line.strip() == f"{probe}:" for line in lines):
            problems.append(f"R2: {sts} has no {probe}")
    paths = values(lines, "path")
    if not paths or any(p != "/healthz" for p in paths):
        problems.append(f"R2: {sts}'s probes are on {paths}, not /healthz")
    if not has("containerPort", "8080"):
        problems.append(f"R2: {sts} does not name port 8080 for /healthz")
    problems += pod_hardened(sts)

    svc = find(docs, "Service", "logbook")
    if svc is None or values(svc.lines, "clusterIP") != ["None"] or values(lines, "serviceName") != ["logbook"]:
        problems.append(f"R2: {sts} has no headless Service/logbook as its serviceName")
    return problems


def pod_hardened(doc: Doc) -> list[str]:
    """What logbook's and radio's pods share: no token, the default seccomp profile,
    non-root on a read-only root file system, requests and a memory limit."""
    problems = []
    lines = doc.lines
    for key, value, what in (
        ("automountServiceAccountToken", "false", "mounts a service account token"),
        ("type", "RuntimeDefault", "does not run with the RuntimeDefault seccomp profile"),
        ("runAsNonRoot", "true", "may run as root"),
        ("readOnlyRootFilesystem", "true", "has a writable root file system"),
    ):
        if value not in values(lines, key):
            problems.append(f"pod: {doc} {what}")
    resources = next((i for i, line in enumerate(lines) if line.strip() == "resources:"), None)
    block = lines[resources:resources + 6] if resources is not None else []
    if not any(line.strip() == "requests:" for line in block):
        problems.append(f"R5: {doc} has no resource requests")
    if not any(line.strip() == "limits:" for line in block) or not values(block, "memory"):
        problems.append(f"R5: {doc} has no memory limit")
    return problems


def radio(docs: list[Doc]) -> list[str]:
    dep = find(docs, "Deployment", "radio")
    if dep is None:
        return ["R2: no Deployment/radio in the render"]
    problems = []
    lines = dep.lines
    if values(lines, "replicas") != ["1"]:
        problems.append(f"replicas: {dep} has replicas {values(lines, 'replicas')}, not exactly 1: one speaker")
    if block(lines, "strategy") != ["type: Recreate"]:
        problems.append(f"R2: {dep} is not replaced by Recreate, so two pods would speak at once")
    for name, want in (("TWR_NATS_URL", "nats://nats:4222"), ("TWR_HEALTH_ADDR", ":8080"),
                       ("TWR_RADIO_SRC", "live"), ("TWR_RADIO_SINK", "console")):
        if env_value(lines, name) != want:
            problems.append(f"R2: {dep}'s {name} is {env_value(lines, name)}, not {want}")
    if any("secretKeyRef:" in line or "secretRef:" in line or "TWR_DISCORD_" in line for line in lines):
        problems.append(f"R3: {dep} reads a Secret or a Discord setting, which {RADIO_DISCORD} adds")
    stripped = {line.strip().removeprefix("- ") for line in lines}
    if "mountPath: /tmp" not in stripped or "emptyDir:" not in stripped:
        problems.append(f"R2: {dep} has no emptyDir at /tmp for its speech cache")
    for probe in ("readinessProbe", "livenessProbe"):
        if not any(line.strip() == f"{probe}:" for line in lines):
            problems.append(f"R2: {dep} has no {probe}")
    paths = values(lines, "path")
    if not paths or any(p != "/healthz" for p in paths):
        problems.append(f"R2: {dep}'s probes are on {paths}, not /healthz")
    if "8080" not in values(lines, "containerPort"):
        problems.append(f"R2: {dep} does not name port 8080 for /healthz")
    problems += pod_hardened(dep)

    svc = find(docs, "Service", "radio")
    if svc is None or values(svc.lines, "port") != ["8080"]:
        problems.append("R2: no Service/radio on 8080")
    return problems


def podium(docs: list[Doc]) -> list[str]:
    dep = find(docs, "Deployment", "podium")
    if dep is None:
        return ["#67: no Deployment/podium in the render"]
    problems = []
    lines = dep.lines
    if values(lines, "replicas") != ["1"]:
        problems.append(f"replicas: {dep} has replicas {values(lines, 'replicas')}, not exactly 1: one poster")
    if block(lines, "strategy") != ["type: Recreate"]:
        problems.append(f"#67: {dep} is not replaced by Recreate, so two pods would post at once")
    for name, want in (("TWR_NATS_URL", "nats://nats:4222"), ("ADDR", ":8080"),
                       ("TWR_PODIUM_SRC", "live"), ("TWR_PODIUM_SINK", "discord")):
        if env_value(lines, name) != want:
            problems.append(f"#67: {dep}'s {name} is {env_value(lines, name)}, not {want}")
    if any("secretKeyRef:" in line or "secretRef:" in line or "TWR_DISCORD_" in line for line in lines):
        problems.append(f"#67: {dep} reads a Secret or a Discord setting, which {PODIUM_DISCORD} adds")
    for probe in ("readinessProbe", "livenessProbe"):
        if not any(line.strip() == f"{probe}:" for line in lines):
            problems.append(f"#67: {dep} has no {probe}")
    paths = values(lines, "path")
    if not paths or any(p != "/healthz" for p in paths):
        problems.append(f"#67: {dep}'s probes are on {paths}, not /healthz")
    if "8080" not in values(lines, "containerPort"):
        problems.append(f"#67: {dep} does not name port 8080 for /healthz")
    problems += pod_hardened(dep)

    svc = find(docs, "Service", "podium")
    if svc is None or values(svc.lines, "port") != ["8080"]:
        problems.append("#67: no Service/podium on 8080")
    return problems


def pitwall(docs: list[Doc]) -> list[str]:
    dep = find(docs, "Deployment", "pitwall")
    if dep is None:
        return ["#132: no Deployment/pitwall in the render"]
    problems = []
    lines = dep.lines
    if values(lines, "replicas") != ["1"]:
        problems.append(f"replicas: {dep} has replicas {values(lines, 'replicas')}, not 1")
    for name, want in (("TWR_NATS_URL", "nats://nats:4222"), ("ADDR", ":8080"),
                       ("TWR_PITWALL_SRC", "live,v1-import")):
        if env_value(lines, name) != want:
            problems.append(f"#132: {dep}'s {name} is {env_value(lines, name)}, not {want}")
    if any(line.strip() in ("volumeMounts:", "volumes:") for line in lines):
        problems.append(f"#132: {dep} mounts a volume, and the pit wall holds no data")
    for probe in ("readinessProbe", "livenessProbe"):
        if not any(line.strip() == f"{probe}:" for line in lines):
            problems.append(f"#132: {dep} has no {probe}")
    paths = values(lines, "path")
    if not paths or any(p != "/healthz" for p in paths):
        problems.append(f"#132: {dep}'s probes are on {paths}, not /healthz")
    if "8080" not in values(lines, "containerPort"):
        problems.append(f"#132: {dep} does not name port 8080")
    problems += pod_hardened(dep)

    svc = find(docs, "Service", "pitwall")
    if svc is None or values(svc.lines, "port") != ["8080"]:
        problems.append("#132: no Service/pitwall on 8080")
    return problems


def pitwall_ingress(docs: list[Doc]) -> list[str]:
    ingress = find(docs, "Ingress", "pitwall")
    if ingress is None:
        return [f"#132: no Ingress/pitwall through {PITWALL_INGRESS}"]
    problems = []
    for key in ("host", "ingressClassName", "tls", "secretName"):
        if any(re.match(rf"^\s*(- )?{key}:", line) for line in ingress.lines):
            problems.append(f"#132: {ingress} sets {key}, which the overlay sets")
    if values(ingress.lines, "name")[1:] != ["pitwall", "http"]:
        problems.append(f"#132: {ingress} does not go to the pitwall Service's http port")
    return problems


def podium_discord(docs: list[Doc]) -> list[str]:
    dep = find(docs, "Deployment", "podium")
    if dep is None:
        return [f"#67: no Deployment/podium through {PODIUM_DISCORD}"]
    problems = []
    lines = dep.lines
    if env_value(lines, "TWR_PODIUM_SINK") != "discord":
        problems.append(f"#67: {dep}'s TWR_PODIUM_SINK is {env_value(lines, 'TWR_PODIUM_SINK')}, not discord")
    for key in PODIUM_SECRET_KEYS:
        i = next((i for i, line in enumerate(lines) if line.strip() == f"- name: {key}"), None)
        head = [line.strip() for line in lines[i + 1:i + 3]] if i is not None else []
        # The whole secretKeyRef, every line indented under it, so a key it gains
        # (an `optional: true` on the token) is seen.
        got: list[str] = []
        if i is not None and head == ["valueFrom:", "secretKeyRef:"]:
            depth = len(lines[i + 2]) - len(lines[i + 2].lstrip())
            for line in lines[i + 3:]:
                if len(line) - len(line.lstrip()) <= depth:
                    break
                got.append(line.strip())
        want = [f"key: {key}", f"name: {RADIO_SECRET}"]
        if key == "TWR_DISCORD_RESULTS_CHANNEL_ID":
            want.append("optional: true")
        if head != ["valueFrom:", "secretKeyRef:"] or sorted(got) != sorted(want):
            problems.append(f"#67: {dep}'s {key} is not read from Secret {RADIO_SECRET}, key {key}: {head + got}")
    secret = find(docs, "Secret", RADIO_SECRET)
    if secret is not None:
        problems.append(f"#67: {secret} is in the render; the overlay provides it")
    return problems


def radio_discord(docs: list[Doc]) -> list[str]:
    dep = find(docs, "Deployment", "radio")
    if dep is None:
        return [f"R3: no Deployment/radio through {RADIO_DISCORD}"]
    problems = []
    lines = dep.lines
    if env_value(lines, "TWR_RADIO_SINK") != "discord":
        problems.append(f"R3: {dep}'s TWR_RADIO_SINK is {env_value(lines, 'TWR_RADIO_SINK')}, not discord")
    for key in RADIO_SECRET_KEYS:
        i = next((i for i, line in enumerate(lines) if line.strip() == f"- name: {key}"), None)
        ref = [line.strip() for line in lines[i + 1:i + 5]] if i is not None else []
        if ref[:2] != ["valueFrom:", "secretKeyRef:"] or sorted(ref[2:4]) != sorted(
            [f"key: {key}", f"name: {RADIO_SECRET}"]
        ):
            problems.append(f"R3: {dep}'s {key} is not read from Secret {RADIO_SECRET}, key {key}: {ref}")
    secret = find(docs, "Secret", RADIO_SECRET)
    if secret is not None:
        problems.append(f"R3: {secret} is in the render; the overlay provides it")
    return problems


def network_policy(docs: list[Doc]) -> list[str]:
    problems = []
    nats = find(docs, "NetworkPolicy", "nats")
    book = find(docs, "NetworkPolicy", "logbook")
    if nats is None or book is None:
        return [f"R7: no NetworkPolicy/nats and NetworkPolicy/logbook through {NETWORK_POLICY}"]
    if not {"app.kubernetes.io/name: nats", "app.kubernetes.io/component: nats"} <= {line.strip() for line in nats.lines}:
        problems.append(f"R7: {nats} does not select NATS's server")
    if "app.kubernetes.io/name: logbook" not in {line.strip() for line in book.lines}:
        problems.append(f"R7: {book} does not select logbook")
    for doc in (nats, book):
        if "- podSelector: {}" not in {line.strip() for line in doc.lines}:
            problems.append(f"R7: {doc} does not let in the namespace's own pods")
    if f"kubernetes.io/metadata.name: {INGRESS_NAMESPACE}" not in {line.strip() for line in nats.lines}:
        problems.append(f"R7: {nats} does not let in the ingress controller's namespace ({INGRESS_NAMESPACE})")
    if values(nats.lines, "port") != ["8080"]:
        problems.append(f"R7: {nats} opens ports {values(nats.lines, 'port')} to the ingress controller, not 8080 only")
    if any("namespaceSelector:" in line for line in book.lines):
        problems.append(f"R7: {book} lets in another namespace")
    for doc in (nats, book):
        if block(doc.lines, "policyTypes") != ["- Ingress"]:
            problems.append(f"R7: {doc}'s policyTypes are {block(doc.lines, 'policyTypes')}, not Ingress only")
        if sum("podSelector: {}" in line for line in doc.lines) != 1 or any("ipBlock:" in line for line in doc.lines):
            problems.append(f"R7: {doc} lets in more than the namespace's own pods and the ingress controller")
    selector = block(nats.lines, "namespaceSelector")
    if sum("namespaceSelector:" in line for line in nats.lines) != 1 or selector != [
        "matchLabels:", f"kubernetes.io/metadata.name: {INGRESS_NAMESPACE}"
    ]:
        problems.append(f"R7: {nats} lets in other namespaces than the ingress controller's: {selector}")
    return problems


def block(lines: list[str], key: str) -> list[str]:
    """The stripped lines under the first `key:` line: those indented deeper, and the
    items of a list kustomize writes at the key's own indentation."""
    for i, line in enumerate(lines):
        m = re.match(rf"^(\s*)(- )?{re.escape(key)}:$", line)
        if not m:
            continue
        indent = len(m.group(1))
        out = []
        for following in lines[i + 1 :]:
            depth = len(following) - len(following.lstrip())
            same_level_item = depth == indent and not m.group(2) and following.lstrip().startswith("- ")
            if depth <= indent and not same_level_item:
                break
            out.append(following.strip())
        return out
    return []


def ecu_live(docs: list[Doc], compose: str) -> list[str]:
    """deploy/examples/ecu-live (paddock#108): the live engineer an overlay copies."""
    problems: list[str] = labelled(docs) + requests(docs)
    dep = find(docs, "Deployment", "ecu")
    if dep is None:
        return problems + ["ecu: no Deployment/ecu in the render"]
    lines = dep.lines
    if values(lines, "replicas") != ["1"]:
        problems.append(f"replicas: {dep} has replicas {values(lines, 'replicas')}, not exactly 1: one listener")
    if block(lines, "strategy") != ["type: Recreate"]:
        problems.append(f"ecu: {dep} is not replaced by Recreate, so a second pod could never take the port")

    # Each port of the container, as the stripped lines of its list item.
    ports: list[list[str]] = []
    for line in block(lines, "ports"):
        if line.startswith("- "):
            ports.append([line[2:]])
        elif ports:
            ports[-1].append(line)
    if not any({"containerPort: 33740", "hostPort: 33740", "protocol: UDP"} <= set(p) for p in ports):
        problems.append(f"ecu: {dep} does not take UDP 33740 as a hostPort")

    for name, want in (("TWR_NATS_URL", "nats://nats:4222"), ("ADDR", ":8080"), ("TWR_SRC", "live"),
                       ("TWR_ECU_PS5_IP", PS5_PLACEHOLDER)):
        if env_value(lines, name) != want:
            problems.append(f"ecu: {dep}'s {name} is {env_value(lines, name)}, not {want}")
    recordings = env_value(lines, "TWR_ECU_RECORDINGS_DIR")
    if recordings is None or recordings not in values(lines, "mountPath") or \
            not any(line.strip() == "persistentVolumeClaim:" for line in lines):
        problems.append(f"ecu: {dep}'s TWR_ECU_RECORDINGS_DIR ({recordings}) is not on a volume that is kept")
    for probe in ("readinessProbe", "livenessProbe"):
        if not any(line.strip() == f"{probe}:" for line in lines):
            problems.append(f"ecu: {dep} has no {probe}")
    paths = values(lines, "path")
    if not paths or any(p != "/healthz" for p in paths):
        problems.append(f"ecu: {dep}'s probes are on {paths}, not /healthz")
    problems += pod_hardened(dep)

    composed = set(images(compose.splitlines()))
    for image in images(lines):
        if image not in composed:
            problems.append(f"ecu: the example runs {image}, which deploy/compose/compose.yaml does not")
    return problems


def nats_config(where: str, conf: dict) -> list[str]:
    problems = []
    if "cluster" in conf:
        problems.append(f"R2: {where} configures a cluster")
    js = conf.get("jetstream", {})
    if js.get("store_dir") != "/data" or js.get("max_memory_store") != 0:
        problems.append(f"R2: {where}'s JetStream is not file storage at /data with memory off: {js}")
    if js.get("max_file_store") != "$NATS_JS_MAX_FILE_STORE":
        problems.append(f"R2: {where}'s max_file_store is {js.get('max_file_store')}, not $NATS_JS_MAX_FILE_STORE")
    ws = conf.get("websocket", {})
    if ws.get("no_tls") is not True or "tls" in ws:
        problems.append(f"R3: {where}'s WebSocket listener has TLS, which ends at the Ingress")

    users = {u.get("user"): u for u in conf.get("authorization", {}).get("users", [])}
    if conf.get("no_auth_user") != "twr" or ws.get("no_auth_user") != "ws":
        problems.append(f"R6: {where}: a client with no credentials is not twr on 4222 and ws on the WebSocket")
    if set(users) != {"twr", "ws"}:
        problems.append(f"R6: {where} has users {sorted(users)}, not twr and ws")
        return problems
    if users["twr"].get("allowed_connection_types") != ["STANDARD"]:
        problems.append(f"R6: {where}: twr may connect other than on 4222")
    ws_user = users["ws"]
    if ws_user.get("allowed_connection_types") != ["WEBSOCKET"]:
        problems.append(f"R6: {where}: ws may connect other than on the WebSocket")
    perms = ws_user.get("permissions", {})
    if perms.get("publish") != {"deny": [">"]}:
        problems.append(f"R6: {where}: ws may publish: {perms.get('publish')}")
    if perms.get("subscribe") != {"allow": ["twr.>"]}:
        problems.append(f"R6: {where}: ws may subscribe to {perms.get('subscribe')}")
    if any(k in u for u in users.values() for k in ("password", "nkey")):
        problems.append(f"R6: {where} has a credential in the config, where a Secret belongs")
    return problems


def check(kustomization: str, text: str, compose: str | None = None) -> list[str]:
    docs = documents(text)
    problems = portable(docs)
    k = kustomization.rstrip("/")
    if k == NATS_CHART:
        problems += nats_chart(docs)
    if k == BASE:
        if compose is None:
            compose = COMPOSE.read_text(encoding="utf-8")
        problems += base(docs, compose)
    if k == NATS_WEBSOCKET:
        problems += labelled(docs) + nats_websocket(docs)
    if k == NETWORK_POLICY:
        problems += labelled(docs) + network_policy(docs)
    if k == RADIO_DISCORD:
        problems += labelled(docs) + radio_discord(docs)
    if k == PODIUM_DISCORD:
        problems += labelled(docs) + podium_discord(docs)
    if k == PITWALL_INGRESS:
        problems += labelled(docs) + pitwall_ingress(docs)
    if k == ECU_LIVE:
        if compose is None:
            compose = COMPOSE.read_text(encoding="utf-8")
        problems = [p for p in problems if p != ECU_HOST_PORT] + ecu_live(docs, compose)
    return problems


def main(argv: list[str]) -> int:
    args = argv[1:]
    namespace = None
    if args[:1] == ["--namespace"] and len(args) >= 2:
        namespace, args = args[1], args[2:]
    if len(args) != 2:
        print("usage: deploy_check.py [--namespace NS] KUSTOMIZATION RENDERED.yaml", file=sys.stderr)
        return 2
    kustomization, rendered = args
    with open(rendered, encoding="utf-8") as f:
        text = f.read()
    if namespace is None:
        problems = check(kustomization, text)
    else:
        problems = namespaced(documents(text), namespace)
    for p in problems:
        print(f"{kustomization}: {p}", file=sys.stderr)
    if problems:
        return 1
    what = f"every object in {namespace} through an overlay" if namespace else "ok"
    print(f"deploy-check: {kustomization} {what}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
