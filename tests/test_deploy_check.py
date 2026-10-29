"""The render check (scripts/deploy_check.py, paddock#42, #44): a public base names no
namespace, storage class, node or host port and pins every image, NATS's chart
renders one server with JetStream on file storage, 4222 and the WebSocket in the
cluster only, and a WebSocket client that may only subscribe (R6), and the base
(#44) runs compose's images and logbook as one StatefulSet replica whose volume is
kept. BASE is GOOD with logbook and the base's label, and POLICIES what
deploy/components/network-policy adds to it.

GOOD is a cut-down `kustomize build --enable-helm deploy/nats/chart`, in the style
kustomize writes, and INGRESS what deploy/components/nats-websocket adds to it; each
test breaks one thing in them. `make deploy-render` runs the
check on the real render."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import re
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

_spec = importlib.util.spec_from_file_location("deploy_check", ROOT / "scripts" / "deploy_check.py")
assert _spec is not None and _spec.loader is not None
deploy_check = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(deploy_check)

NATS = "deploy/nats/chart"
BOX = "docker.io/natsio/nats-box:0.20.0@sha256:" + "d" * 64

REQUESTS = "        resources:\n          requests:\n            cpu: 10m\n"

GOOD = f"""\
apiVersion: v1
data:
  nats.conf: |
    {{
      "authorization": {{
        "users": [
          {{
            "allowed_connection_types": [
              "STANDARD"
            ],
            "user": "twr"
          }},
          {{
            "allowed_connection_types": [
              "WEBSOCKET"
            ],
            "permissions": {{
              "publish": {{
                "deny": [
                  ">"
                ]
              }},
              "subscribe": {{
                "allow": [
                  "twr.>"
                ]
              }}
            }},
            "user": "ws"
          }}
        ]
      }},
      "http_port": 8222,
      "jetstream": {{
        "max_file_store": $NATS_JS_MAX_FILE_STORE,
        "max_memory_store": 0,
        "store_dir": "/data"
      }},
      "no_auth_user": "twr",
      "port": 4222,
      "server_name": $SERVER_NAME,
      "websocket": {{
        "no_auth_user": "ws",
        "no_tls": true,
        "port": 8080
      }}
    }}
kind: ConfigMap
metadata:
  labels:
    app.kubernetes.io/name: nats
  name: nats-config
---
apiVersion: v1
kind: Service
metadata:
  name: nats
spec:
  ports:
  - appProtocol: tcp
    name: nats
    port: 4222
    targetPort: nats
  - appProtocol: http
    name: websocket
    port: 8080
    targetPort: websocket
---
apiVersion: v1
kind: Service
metadata:
  name: nats-headless
spec:
  clusterIP: None
  ports:
  - name: monitor
    port: 8222
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nats-box
spec:
  template:
    spec:
      automountServiceAccountToken: false
      containers:
      - image: {BOX}
        name: nats-box
        resources:
          requests:
            cpu: 10m
      securityContext:
        seccompProfile:
          type: RuntimeDefault
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nats
spec:
  replicas: 1
  template:
    spec:
      automountServiceAccountToken: false
      containers:
      - env:
        - name: NATS_JS_MAX_FILE_STORE
          value: 5Gi
        image: docker.io/library/nats:2.15.1-alpine@sha256:{"a" * 64}
        name: nats
        resources:
          requests:
            cpu: 10m
      - image: docker.io/natsio/prometheus-nats-exporter:0.20.2@sha256:{"b" * 64}
        name: prom-exporter
        resources:
          requests:
            cpu: 10m
      securityContext:
        seccompProfile:
          type: RuntimeDefault
  volumeClaimTemplates:
  - metadata:
      name: nats-js
    spec:
      resources:
        requests:
          storage: 5Gi
"""

# What deploy/components/nats-websocket adds to the base.
INGRESS = """\
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nats-ws
spec:
  rules:
  - http:
      paths:
      - backend:
          service:
            name: nats
            port:
              name: websocket
        path: /
        pathType: Prefix
"""


def problems(text: str, kustomization: str = NATS) -> list[str]:
    return deploy_check.check(kustomization, text)


def broken(old: str, new: str) -> str:
    assert GOOD.count(old) == 1, old
    return GOOD.replace(old, new)


class Render(unittest.TestCase):
    def assertFails(self, text: str, rule: str) -> None:
        found = problems(text)
        self.assertTrue(any(p.startswith(rule + ":") for p in found), found)

    def test_good(self) -> None:
        self.assertEqual(problems(GOOD), [])

    def test_documents(self) -> None:
        docs = deploy_check.documents(GOOD)
        self.assertEqual(
            [str(d) for d in docs],
            ["ConfigMap/nats-config", "Service/nats", "Service/nats-headless",
             "Deployment/nats-box", "StatefulSet/nats"],
        )

    def test_portable(self) -> None:
        for line in ("  namespace: twr", "  storageClassName: my-storage-class",
                     "  nodeSelector: {}", "  hostNetwork: true", "  - hostPort: 33740",
                     "  type: LoadBalancer"):
            with self.subTest(line=line):
                self.assertFails(broken("  replicas: 1\n", f"  replicas: 1\n{line}\n"), "portable")

    def test_pinned(self) -> None:
        self.assertFails(broken(BOX, "docker.io/natsio/nats-box:0.20.0"), "pinned")
        self.assertFails(broken(BOX, "docker.io/natsio/nats-box@sha256:" + "d" * 64), "pinned")

    def test_other_kustomizations_get_only_the_portable_rules(self) -> None:
        self.assertEqual(problems("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n", "deploy/nats"), [])

    def test_missing_objects(self) -> None:
        self.assertFails(broken("metadata:\n  name: nats-box\n", "metadata:\n  name: other\n"), "nats")

    def test_r2(self) -> None:
        self.assertFails(broken("  replicas: 1\n", "  replicas: 3\n"), "R2")
        self.assertFails(broken('"max_memory_store": 0', '"max_memory_store": 1024'), "R2")
        self.assertFails(broken("$NATS_JS_MAX_FILE_STORE,", '"5Gi",'), "R2")
        self.assertFails(broken("          value: 5Gi", "          value: 10Gi"), "R2")
        self.assertFails(broken('"http_port": 8222,', '"cluster": {},\n      "http_port": 8222,'), "R2")

    def test_r3(self) -> None:
        self.assertFails(broken("    port: 8080\n", "    port: 8080\n  - name: monitor\n    port: 8222\n"), "R3")
        self.assertFails(broken('"no_tls": true', '"no_tls": false'), "R3")
        # The base exposes nothing: the Ingress comes only with the component.
        self.assertFails(GOOD + INGRESS, "R3")

    def test_r4(self) -> None:
        self.assertFails(broken("        name: prom-exporter", "        name: other"), "R4")

    def test_pods(self) -> None:
        self.assertFails(GOOD.replace("automountServiceAccountToken: false", "automountServiceAccountToken: true", 1), "pod")
        self.assertFails(GOOD.replace("automountServiceAccountToken: false\n", "", 1), "pod")
        self.assertFails(GOOD.replace("type: RuntimeDefault", "type: Unconfined", 1), "pod")
        self.assertEqual(GOOD.count("type: RuntimeDefault"), 2)

    def test_r6(self) -> None:
        self.assertFails(broken('"no_auth_user": "ws"', '"no_auth_user": "twr"'), "R6")
        self.assertFails(broken('"STANDARD"', '"STANDARD", "WEBSOCKET"'), "R6")
        self.assertFails(broken('"deny": [\n                  ">"\n                ]', '"allow": [">"]'), "R6")
        self.assertFails(broken('"twr.>"\n', '"twr.>",\n                  "_INBOX.>"\n'), "R6")
        self.assertFails(broken('"twr.>"\n', '"twr.>",\n                  "$JS.API.>"\n'), "R6")
        self.assertFails(broken('"user": "twr"', '"user": "twr", "password": "x"'), "R6")


class WebSocketComponent(unittest.TestCase):
    """deploy/components/nats-websocket, rendered over the base."""

    def test_label(self) -> None:
        found = self.check(labelled(GOOD) + INGRESS)
        self.assertEqual([p for p in found if p.startswith("R1:")],
                         ["R1: Ingress/nats-ws has no label app.kubernetes.io/part-of: twr"])

    def check(self, text: str) -> list[str]:
        return deploy_check.check(deploy_check.NATS_WEBSOCKET, text)

    def test_good(self) -> None:
        self.assertEqual(self.check(labelled(GOOD + INGRESS)), [])

    def test_no_ingress(self) -> None:
        self.assertTrue(any(p.startswith("R3:") for p in self.check(GOOD)))

    def test_nothing_the_overlay_sets(self) -> None:
        for old, new in (("  - http:\n", "  - host: nats.example.com\n    http:\n"),
                         ("  rules:\n", "  ingressClassName: my-ingress-class\n  rules:\n"),
                         ("  rules:\n", "  tls:\n  - secretName: twr-nats-tls\n  rules:\n")):
            with self.subTest(new=new):
                found = self.check(GOOD + INGRESS.replace(old, new))
                self.assertTrue(any(p.startswith("R3:") for p in found), found)

    def test_backend(self) -> None:
        found = self.check(GOOD + INGRESS.replace("              name: websocket", "              name: nats"))
        self.assertTrue(any(p.startswith("R3:") for p in found), found)

    def test_the_component_is_a_component(self) -> None:
        text = (ROOT / deploy_check.NATS_WEBSOCKET / "kustomization.yaml").read_text()
        self.assertIn("\nkind: Component\n", text)
        chart = (ROOT / NATS / "kustomization.yaml").read_text()
        self.assertNotIn("ingress.yaml", chart)


def in_namespace(text: str, namespace: str) -> str:
    """GOOD as an overlay with `namespace:` renders it: each object's metadata names it."""
    return text.replace("metadata:\n", f"metadata:\n  namespace: {namespace}\n")


class NamespaceProbe(unittest.TestCase):
    """The overlay probe (kustomize#6058): every namespaced object takes the overlay's
    namespace."""

    def check(self, text: str) -> list[str]:
        return deploy_check.namespaced(deploy_check.documents(text), "probe")

    def test_every_object_in_the_namespace(self) -> None:
        self.assertEqual(self.check(in_namespace(GOOD + INGRESS, "probe")), [])

    def test_objects_left_out(self) -> None:
        # What kustomize 5.8 renders: the overlay's namespace on the Ingress only.
        text = in_namespace(GOOD + INGRESS, "probe").replace("  namespace: probe\n", "", 5)
        found = self.check(text)
        self.assertEqual(len(found), 5, found)
        self.assertTrue(all(p.startswith("namespace: ") for p in found))
        self.assertIn("ConfigMap/nats-config", found[0])

    def test_another_namespace(self) -> None:
        self.assertEqual(len(self.check(in_namespace(GOOD + INGRESS, "other"))), 6)

    def test_cluster_scoped_kinds_are_left_alone(self) -> None:
        text = "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: probe\n"
        self.assertEqual(self.check(text), [])

    def test_a_label_named_namespace_is_not_metadata(self) -> None:
        text = ("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  labels:\n    namespace: probe\n"
                "  name: x\ndata:\n  namespace: probe\n")
        self.assertEqual(len(self.check(text)), 1)

    def test_main(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            good = Path(d, "good.yaml")
            good.write_text(in_namespace(GOOD, "probe"))
            bad = Path(d, "bad.yaml")
            bad.write_text(GOOD)
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(deploy_check.main(["x", "--namespace", "probe", NATS, str(good)]), 0)
                self.assertEqual(deploy_check.main(["x", "--namespace", "probe", NATS, str(bad)]), 1)
                self.assertEqual(deploy_check.main(["x", NATS, str(good.with_name("good.yaml"))]), 1)
                self.assertEqual(deploy_check.main(["x", "--namespace"]), 2)


class ChartImages(unittest.TestCase):
    def test_nats_and_nats_box_are_composes(self) -> None:
        """The cluster runs the NATS and nats-box compose runs and nats-it tests."""
        values = (ROOT / "deploy" / "nats" / "chart" / "values.yaml").read_text()
        compose = (ROOT / "deploy" / "compose" / "compose.yaml").read_text()
        images = [line.split(": ", 1)[1].strip() for line in values.splitlines()
                  if line.strip().startswith("fullImageName:")]
        self.assertEqual(len(images), 3, images)
        for image in images:
            self.assertRegex(image, deploy_check.PINNED)
            if "prometheus-nats-exporter" not in image:
                self.assertIn(f"image: {image}\n", compose)


# logbook, as deploy/base/logbook.yaml renders, and what compose runs.
LOGBOOK_IMAGE = "ghcr.io/those-who-run/twr-logbook:0.1.3@sha256:" + "e" * 64

LOGBOOK = f"""\
---
apiVersion: v1
kind: Service
metadata:
  name: logbook
spec:
  clusterIP: None
  ports:
  - name: http
    port: 8080
    targetPort: http
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: logbook
spec:
  persistentVolumeClaimRetentionPolicy:
    whenDeleted: Retain
    whenScaled: Retain
  podManagementPolicy: OrderedReady
  replicas: 1
  serviceName: logbook
  template:
    spec:
      automountServiceAccountToken: false
      containers:
      - env:
        - name: TWR_NATS_URL
          value: nats://nats:4222
        - name: TWR_DATA_DIR
          value: /data
        - name: TWR_HEALTH_ADDR
          value: :8080
        image: {LOGBOOK_IMAGE}
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
        name: logbook
        ports:
        - containerPort: 8080
          name: http
        readinessProbe:
          httpGet:
            path: /healthz
            port: http
        resources:
          limits:
            memory: 1Gi
          requests:
            cpu: 50m
            memory: 128Mi
        securityContext:
          readOnlyRootFilesystem: true
        volumeMounts:
        - mountPath: /data
          name: data
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      resources:
        requests:
          storage: 1Gi
"""


def labelled(text: str) -> str:
    """Each object with the base's common label, as kustomize writes it."""
    text = text.replace("metadata:\n  labels:\n", "metadata:\n  labels:\n    app.kubernetes.io/part-of: twr\n")
    return text.replace("metadata:\n  name:", "metadata:\n  labels:\n    app.kubernetes.io/part-of: twr\n  name:")


BASE = labelled(GOOD + LOGBOOK)
COMPOSE = f"""\
services:
  nats:
    image: docker.io/library/nats:2.15.1-alpine@sha256:{"a" * 64}
  nats-bootstrap:
    image: {BOX}
  logbook:
    image: {LOGBOOK_IMAGE}
"""


class Base(unittest.TestCase):
    """deploy/base (paddock#44): the common label, compose's images, NATS at one
    replica and logbook's StatefulSet."""

    def check(self, text: str, compose: str = COMPOSE) -> list[str]:
        return deploy_check.check(deploy_check.BASE, text, compose)

    def assertFails(self, text: str, rule: str, compose: str = COMPOSE) -> None:
        found = self.check(text, compose)
        self.assertTrue(any(p.startswith(rule + ":") for p in found), found)

    def broken(self, old: str, new: str) -> str:
        self.assertEqual(BASE.count(old), 1, old)
        return BASE.replace(old, new)

    def test_good(self) -> None:
        self.assertEqual(self.check(BASE), [])
        self.assertEqual(BASE.count("app.kubernetes.io/part-of: twr"), 7)

    def test_label(self) -> None:
        text = BASE.replace("  labels:\n    app.kubernetes.io/part-of: twr\n  name: logbook\n", "  name: logbook\n", 1)
        self.assertFails(text, "R1")

    def test_images_are_composes(self) -> None:
        # A bump of the base's logbook alone, or of compose's alone, fails.
        self.assertFails(BASE.replace(LOGBOOK_IMAGE, LOGBOOK_IMAGE.replace("0.1.3", "0.1.4")), "R1")
        self.assertFails(BASE, "R1", COMPOSE.replace("0.1.3", "0.1.4"))
        self.assertFails(BASE, "R1", COMPOSE.replace("a" * 64, "f" * 64))
        # The exporter is the one image compose does not run.
        self.assertFails(BASE.replace("prometheus-nats-exporter", "other-exporter"), "R1")

    def test_compose_replays_with_ecu(self) -> None:
        # ecu's image is compose's alone (its replay service); any other is not.
        replay = f"  replay:\n    image: ghcr.io/those-who-run/twr-ecu:0.1.4@sha256:{'c' * 64}\n"
        self.assertEqual(self.check(BASE, COMPOSE + replay), [])
        self.assertFails(BASE, "R1", COMPOSE + replay.replace("twr-ecu", "twr-radio"))

    def test_nats_one_replica(self) -> None:
        self.assertFails(BASE.replace("  replicas: 1\n", "  replicas: 2\n", 1), "replicas")

    def test_no_ingress(self) -> None:
        self.assertFails(BASE + labelled(INGRESS), "R3")

    def test_logbook_replicas_exactly_one(self) -> None:
        for count in ("0", "2"):
            with self.subTest(count=count):
                text = BASE.replace("  podManagementPolicy: OrderedReady\n  replicas: 1\n",
                                    f"  podManagementPolicy: OrderedReady\n  replicas: {count}\n")
                self.assertFails(text, "replicas")
        self.assertFails(self.broken("  podManagementPolicy: OrderedReady\n  replicas: 1\n",
                                     "  podManagementPolicy: OrderedReady\n"), "replicas")

    def test_retention(self) -> None:
        self.assertFails(self.broken("whenDeleted: Retain", "whenDeleted: Delete"), "retention")
        self.assertFails(self.broken("whenScaled: Retain", "whenScaled: Delete"), "retention")
        self.assertFails(self.broken("  persistentVolumeClaimRetentionPolicy:\n    whenDeleted: Retain\n"
                                     "    whenScaled: Retain\n", ""), "retention")

    def test_no_storage_class(self) -> None:
        text = self.broken("      name: data\n    spec:\n", "      name: data\n    spec:\n      storageClassName: x\n")
        self.assertFails(text, "storage")
        self.assertFails(text, "portable")

    def test_r2(self) -> None:
        for old, new in (("OrderedReady", "Parallel"),
                         ("- mountPath: /data", "- mountPath: /var/lib/logbook"),
                         ("value: nats://nats:4222", "value: nats://localhost:4222"),
                         ("value: :8080", "value: :9090"),
                         ("        readinessProbe:\n", "        otherProbe:\n"),
                         ("        livenessProbe:\n", "        otherProbe:\n"),
                         ("  clusterIP: None\n  ports:\n  - name: http\n", "  ports:\n  - name: http\n"),
                         ("  serviceName: logbook\n", "")):
            with self.subTest(old=old):
                self.assertFails(self.broken(old, new), "R2")
        text = BASE.replace("path: /healthz", "path: /", 1)
        self.assertFails(text, "R2")

    def test_pod(self) -> None:
        sts = BASE.index("name: logbook\nspec:\n  persistent")
        for old, new in (("automountServiceAccountToken: false", "automountServiceAccountToken: true"),
                         ("runAsNonRoot: true", "runAsNonRoot: false"),
                         ("readOnlyRootFilesystem: true", "readOnlyRootFilesystem: false"),
                         ("type: RuntimeDefault", "type: Unconfined")):
            with self.subTest(old=old):
                text = BASE[:sts] + BASE[sts:].replace(old, new)
                self.assertFails(text, "pod")

    def test_r5(self) -> None:
        self.assertFails(self.broken("          limits:\n            memory: 1Gi\n", ""), "R5")
        self.assertFails(self.broken("          requests:\n            cpu: 50m\n            memory: 128Mi\n", ""), "R5")

    def test_every_container_has_requests(self) -> None:
        # R5 holds for every container, not only logbook's.
        for name in ("nats-box", "nats", "prom-exporter"):
            with self.subTest(container=name):
                old = f"        name: {name}\n{REQUESTS}"
                self.assertEqual(BASE.count(old), 1, old)
                found = self.check(BASE.replace(old, f"        name: {name}\n"))
                self.assertTrue(any(p.startswith("R5:") and f"container {name} " in p for p in found), found)

    def test_containers(self) -> None:
        docs = deploy_check.documents(BASE)
        sts = deploy_check.find(docs, "StatefulSet", "nats")
        assert sts is not None
        self.assertEqual([deploy_check.container_name(c) for c in deploy_check.containers(sts.lines)],
                         ["nats", "prom-exporter"])

    def test_missing_logbook(self) -> None:
        self.assertFails(labelled(GOOD), "R2")

    def test_the_base_pins_composes_logbook(self) -> None:
        """deploy/base/kustomization.yaml's images: block is compose's logbook, by tag
        and digest, offline (make deploy-render checks the render)."""
        text = (ROOT / "deploy" / "base" / "kustomization.yaml").read_text()
        tag = re.search(r"^    newTag: (\S+)$", text, re.M)
        digest = re.search(r"^    digest: (sha256:[a-f0-9]{64})$", text, re.M)
        assert tag is not None and digest is not None
        self.assertIn("  - name: ghcr.io/those-who-run/twr-logbook\n", text)
        compose = (ROOT / "deploy" / "compose" / "compose.yaml").read_text()
        self.assertIn(f"image: ghcr.io/those-who-run/twr-logbook:{tag.group(1)}@{digest.group(1)}\n", compose)


# What deploy/components/network-policy adds to the base.
POLICIES = """\
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: nats
spec:
  ingress:
  - from:
    - podSelector: {}
  - from:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: my-ingress-namespace
    ports:
    - port: 8080
      protocol: TCP
  podSelector:
    matchLabels:
      app.kubernetes.io/component: nats
      app.kubernetes.io/name: nats
  policyTypes:
  - Ingress
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: logbook
spec:
  ingress:
  - from:
    - podSelector: {}
  podSelector:
    matchLabels:
      app.kubernetes.io/name: logbook
  policyTypes:
  - Ingress
"""


class NetworkPolicyComponent(unittest.TestCase):
    """deploy/components/network-policy, rendered over the base."""

    def check(self, text: str) -> list[str]:
        return deploy_check.check(deploy_check.NETWORK_POLICY, text)

    def assertFails(self, text: str) -> None:
        found = self.check(text)
        self.assertTrue(any(p.startswith("R7:") for p in found), found)

    def test_good(self) -> None:
        self.assertEqual(self.check(BASE + labelled(POLICIES)), [])

    def test_broken(self) -> None:
        for old, new in (("  name: logbook\nspec:\n  ingress", "  name: other\nspec:\n  ingress"),
                         ("      app.kubernetes.io/component: nats\n", ""),
                         ("    - podSelector: {}\n  - from", "    - podSelector: {app: x}\n  - from"),
                         ("my-ingress-namespace", "kube-system\n          other: x"),
                         ("    - port: 8080\n", "    - port: 4222\n"),
                         ("    ports:\n    - port: 8080\n      protocol: TCP\n", ""),
                         ("  - from:\n    - podSelector: {}\n  podSelector:\n    matchLabels:\n      app.kubernetes.io/name: logbook",
                          "  - from:\n    - namespaceSelector: {}\n    - podSelector: {}\n  podSelector:\n    matchLabels:\n      app.kubernetes.io/name: logbook")):
            with self.subTest(old=old):
                self.assertEqual(POLICIES.count(old), 1, old)
                self.assertFails(BASE + POLICIES.replace(old, new))

    def test_ingress_only(self) -> None:
        # Both policies stay Ingress only: an Egress type with no egress rules would cut
        # NATS and logbook off from everything, the probes' DNS included.
        for nth in (1, 2):
            with self.subTest(policy=nth):
                parts = POLICIES.split("  policyTypes:\n  - Ingress\n")
                self.assertEqual(len(parts), 3)
                text = "  policyTypes:\n  - Ingress\n".join(parts[:nth]) + \
                    "  policyTypes:\n  - Ingress\n  - Egress\n" + "  policyTypes:\n  - Ingress\n".join(parts[nth:])
                self.assertFails(BASE + text)

    def test_only_the_ingress_namespace(self) -> None:
        # NATS's second rule names the ingress controller's namespace and nothing more.
        selector = "        matchLabels:\n          kubernetes.io/metadata.name: my-ingress-namespace\n"
        for new in ("        {}\n",
                    selector + "          other: x\n",
                    "        matchExpressions:\n        - key: kubernetes.io/metadata.name\n          operator: Exists\n",
                    selector + "    - ipBlock:\n        cidr: 0.0.0.0/0\n"):
            with self.subTest(new=new):
                self.assertEqual(POLICIES.count(selector), 1)
                self.assertFails(BASE + POLICIES.replace(selector, new))
        # A second namespace beside it, as its own peer.
        self.assertFails(BASE + POLICIES.replace(
            "    - namespaceSelector:\n", "    - namespaceSelector: {}\n    - namespaceSelector:\n"))
        # Another pod selector beside the namespace's own pods.
        self.assertFails(BASE + POLICIES.replace(
            "    - podSelector: {}\n  - from", "    - podSelector: {}\n      namespaceSelector: {}\n  - from"))

    def test_block(self) -> None:
        lines = POLICIES.splitlines()
        self.assertEqual(deploy_check.block(lines, "policyTypes"), ["- Ingress"])
        self.assertEqual(deploy_check.block(lines, "namespaceSelector"),
                         ["matchLabels:", "kubernetes.io/metadata.name: my-ingress-namespace"])

    def test_label(self) -> None:
        # The base's labels do not reach a component's objects (paddock#295).
        found = self.check(BASE + POLICIES)
        self.assertEqual(len([p for p in found if p.startswith("R1:")]), 2, found)

    def test_no_policies(self) -> None:
        self.assertFails(BASE)

    def test_the_component_is_a_component(self) -> None:
        text = (ROOT / deploy_check.NETWORK_POLICY / "kustomization.yaml").read_text()
        self.assertIn("\nkind: Component\n", text)
        base = (ROOT / deploy_check.BASE / "kustomization.yaml").read_text()
        self.assertNotIn("network-policy", base.split("resources:", 1)[1].split("labels:", 1)[0])


if __name__ == "__main__":
    unittest.main()
