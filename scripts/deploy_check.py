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
  monitoring 8222 is on no Service but the headless one; the WebSocket has no TLS,
  and the Ingress ``nats-ws`` goes to it with no host, class or TLS of its own;
- R4: the exporter beside the server, and the ``nats-box`` Deployment;
- R6: a client with no credentials is ``twr`` on 4222 and ``ws`` on the WebSocket;
  ``twr`` is allowed only standard connections, and ``ws`` only WebSocket ones, may
  publish nothing and may subscribe to ``twr.>`` and ``_INBOX.>`` only.

kustomize writes a fixed, block style of YAML, and this reads it line by line,
standard library only as paddock's other checks are. The server's config is the
JSON the chart writes into the ``nats-config`` ConfigMap, read as JSON.
"""

from __future__ import annotations

import json
import re
import sys

NATS_CHART = "deploy/nats/chart"

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
    ingress = need("Ingress", "nats-ws")
    need("Deployment", "nats-box")

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

    if ingress is not None:
        for key in ("host", "ingressClassName", "tls", "secretName"):
            if any(re.match(rf"^\s*(- )?{key}:", line) for line in ingress.lines):
                problems.append(f"R3: {ingress} sets {key}, which the overlay sets")
        if values(ingress.lines, "name")[1:] != ["nats", "websocket"]:
            problems.append(f"R3: {ingress} does not go to the nats Service's websocket port")
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
    if perms.get("subscribe") != {"allow": ["twr.>", "_INBOX.>"]}:
        problems.append(f"R6: {where}: ws may subscribe to {perms.get('subscribe')}")
    if any(k in u for u in users.values() for k in ("password", "nkey")):
        problems.append(f"R6: {where} has a credential in the config, where a Secret belongs")
    return problems


def check(kustomization: str, text: str) -> list[str]:
    docs = documents(text)
    problems = portable(docs)
    if kustomization.rstrip("/") == NATS_CHART:
        problems += nats_chart(docs)
    return problems


def main(argv: list[str]) -> int:
    if len(argv) != 3:
        print("usage: deploy_check.py KUSTOMIZATION RENDERED.yaml", file=sys.stderr)
        return 2
    with open(argv[2], encoding="utf-8") as f:
        problems = check(argv[1], f.read())
    for p in problems:
        print(f"{argv[1]}: {p}", file=sys.stderr)
    if problems:
        return 1
    print(f"deploy-check: {argv[1]} ok")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
