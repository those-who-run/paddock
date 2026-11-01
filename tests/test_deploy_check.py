"""The render check (scripts/deploy_check.py, paddock#42, #44): a public base names no
namespace, storage class, node or host port and pins every image, NATS's chart
renders one server with JetStream on file storage, 4222 and the WebSocket in the
cluster only, and a WebSocket client that may only subscribe (R6), and the base
(#44) runs compose's images and logbook as one StatefulSet replica whose volume is
kept, and radio (#63) as one Deployment replica on the console, with Discord and its
Secret only through deploy/components/radio-discord. BASE is GOOD with logbook,
radio and the base's label, POLICIES what deploy/components/network-policy adds to
it, and DISCORD radio as deploy/components/radio-discord changes it. podium (#67)
is in the base with its cards off, and PODIUM_DISCORD is podium as
deploy/components/podium-discord changes it. The pit wall (#132) is in the base
with no Ingress, and PITWALL_INGRESS is what deploy/components/pitwall-ingress adds.

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


# radio, as deploy/base/radio-deployment.yaml and radio-service.yaml render.
RADIO_IMAGE = "ghcr.io/those-who-run/twr-radio:0.1.0@sha256:" + "b" * 64

RADIO = f"""\
---
apiVersion: v1
kind: Service
metadata:
  name: radio
spec:
  ports:
  - name: http
    port: 8080
    targetPort: http
  selector:
    app.kubernetes.io/name: radio
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: radio
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: radio
  strategy:
    type: Recreate
  template:
    spec:
      automountServiceAccountToken: false
      containers:
      - env:
        - name: TWR_NATS_URL
          value: nats://nats:4222
        - name: TWR_HEALTH_ADDR
          value: :8080
        - name: TWR_RADIO_SRC
          value: live
        - name: TWR_RADIO_SINK
          value: console
        image: {RADIO_IMAGE}
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
        name: radio
        ports:
        - containerPort: 8080
          name: http
        readinessProbe:
          httpGet:
            path: /healthz
            port: http
        resources:
          limits:
            memory: 2Gi
          requests:
            cpu: 250m
            memory: 256Mi
        securityContext:
          readOnlyRootFilesystem: true
        volumeMounts:
        - mountPath: /tmp
          name: tmp
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      volumes:
      - emptyDir:
          sizeLimit: 256Mi
        name: tmp
"""


# podium, as deploy/base/podium-deployment.yaml and podium-service.yaml render
# (paddock#67).
PODIUM_IMAGE = "ghcr.io/those-who-run/twr-podium:0.1.2@sha256:" + "d" * 64

PODIUM = f"""\
---
apiVersion: v1
kind: Service
metadata:
  name: podium
spec:
  ports:
  - name: http
    port: 8080
    targetPort: http
  selector:
    app.kubernetes.io/name: podium
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: podium
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: podium
  strategy:
    type: Recreate
  template:
    spec:
      automountServiceAccountToken: false
      containers:
      - env:
        - name: TWR_NATS_URL
          value: nats://nats:4222
        - name: ADDR
          value: :8080
        - name: TWR_PODIUM_SRC
          value: live
        - name: TWR_PODIUM_SINK
          value: discord
        image: {PODIUM_IMAGE}
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
        name: podium
        ports:
        - containerPort: 8080
          name: http
        readinessProbe:
          httpGet:
            path: /healthz
            port: http
        resources:
          limits:
            memory: 128Mi
          requests:
            cpu: 10m
            memory: 32Mi
        securityContext:
          readOnlyRootFilesystem: true
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
"""


# The pit wall, as deploy/base/pitwall-deployment.yaml and pitwall-service.yaml
# render (paddock#132).
PITWALL_IMAGE = "ghcr.io/those-who-run/twr-pitwall:0.1.0@sha256:" + "e" * 64

PITWALL = f"""\
---
apiVersion: v1
kind: Service
metadata:
  name: pitwall
spec:
  ports:
  - name: http
    port: 8080
    targetPort: http
  selector:
    app.kubernetes.io/name: pitwall
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pitwall
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: pitwall
  strategy:
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
    type: RollingUpdate
  template:
    spec:
      automountServiceAccountToken: false
      containers:
      - env:
        - name: TWR_NATS_URL
          value: nats://nats:4222
        - name: ADDR
          value: :8080
        - name: TWR_PITWALL_SRC
          value: live,v1-import
        image: {PITWALL_IMAGE}
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
        name: pitwall
        ports:
        - containerPort: 8080
          name: http
        readinessProbe:
          httpGet:
            path: /healthz
            port: http
        resources:
          limits:
            memory: 256Mi
          requests:
            cpu: 10m
            memory: 32Mi
        securityContext:
          readOnlyRootFilesystem: true
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
"""

# What deploy/components/pitwall-ingress adds to the base.
PITWALL_INGRESS = """\
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: pitwall
spec:
  rules:
  - http:
      paths:
      - backend:
          service:
            name: pitwall
            port:
              name: http
        path: /
        pathType: Prefix
"""


def labelled(text: str) -> str:
    """Each object with the base's common label, as kustomize writes it."""
    text = text.replace("metadata:\n  labels:\n", "metadata:\n  labels:\n    app.kubernetes.io/part-of: twr\n")
    return text.replace("metadata:\n  name:", "metadata:\n  labels:\n    app.kubernetes.io/part-of: twr\n  name:")


LOGBOOK_PART = labelled(LOGBOOK)
RADIO_PART = labelled(RADIO)
PODIUM_PART = labelled(PODIUM)
PITWALL_PART = labelled(PITWALL)
BASE = labelled(GOOD) + LOGBOOK_PART + RADIO_PART + PODIUM_PART + PITWALL_PART
COMPOSE = f"""\
services:
  nats:
    image: docker.io/library/nats:2.15.1-alpine@sha256:{"a" * 64}
  nats-bootstrap:
    image: {BOX}
  logbook:
    image: {LOGBOOK_IMAGE}
  radio:
    image: {RADIO_IMAGE}
  podium:
    image: {PODIUM_IMAGE}
  pitwall:
    image: {PITWALL_IMAGE}
"""


class Base(unittest.TestCase):
    """deploy/base (paddock#44): the common label, compose's images, NATS at one
    replica and logbook's StatefulSet; and radio's Deployment (paddock#63)."""

    def check(self, text: str, compose: str = COMPOSE) -> list[str]:
        return deploy_check.check(deploy_check.BASE, text, compose)

    def assertFails(self, text: str, rule: str, compose: str = COMPOSE) -> None:
        found = self.check(text, compose)
        self.assertTrue(any(p.startswith(rule + ":") for p in found), found)

    def broken(self, old: str, new: str, part: str = LOGBOOK_PART) -> str:
        """BASE with old changed to new in one object's part (logbook's by default),
        where it is once."""
        self.assertEqual(part.count(old), 1, old)
        return BASE.replace(part, part.replace(old, new))

    def test_good(self) -> None:
        self.assertEqual(self.check(BASE), [])
        self.assertEqual(BASE.count("app.kubernetes.io/part-of: twr"), 13)

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
        self.assertFails(labelled(GOOD) + RADIO_PART + PODIUM_PART, "R2")

    def test_missing_radio(self) -> None:
        self.assertFails(labelled(GOOD) + LOGBOOK_PART + PODIUM_PART, "R2")
        self.assertFails(labelled(GOOD) + LOGBOOK_PART + PODIUM_PART, "R1")  # compose runs radio

    def test_radio_replicas_exactly_one(self) -> None:
        for count in ("0", "2"):
            with self.subTest(count=count):
                self.assertFails(self.broken("  replicas: 1\n", f"  replicas: {count}\n", RADIO_PART), "replicas")

    def test_radio_r2(self) -> None:
        for old, new in (("    type: Recreate\n", "    type: RollingUpdate\n"),
                         ("  strategy:\n    type: Recreate\n", ""),
                         ("value: nats://nats:4222", "value: nats://localhost:4222"),
                         ("value: :8080", "value: :9090"),
                         ("value: live", "value: replay-x"),
                         ("value: console", "value: wav"),
                         ("- mountPath: /tmp", "- mountPath: /cache"),
                         ("      - emptyDir:\n          sizeLimit: 256Mi\n", "      - hostPath:\n          path: /x\n"),
                         ("        readinessProbe:\n", "        otherProbe:\n"),
                         ("        livenessProbe:\n", "        otherProbe:\n"),
                         ("- containerPort: 8080", "- containerPort: 9090"),
                         ("    port: 8080\n", "    port: 9090\n")):
            with self.subTest(old=old):
                self.assertFails(self.broken(old, new, RADIO_PART), "R2")
        self.assertFails(self.broken("            path: /healthz\n            port: http\n        name:",
                                     "            path: /\n            port: http\n        name:", RADIO_PART), "R2")

    def test_radio_no_discord_in_the_base(self) -> None:
        # R3: the base speaks to the console and reads no Secret; Discord is the
        # component's.
        for env in ("        - name: TWR_DISCORD_VOICE_CHANNEL_ID\n          value: x\n",
                    "        - name: X\n          valueFrom:\n            secretKeyRef:\n              key: k\n              name: s\n"):
            with self.subTest(env=env):
                self.assertFails(self.broken("      - env:\n", "      - env:\n" + env, RADIO_PART), "R3")
        self.assertFails(self.broken("      - env:\n", "      - envFrom:\n        - secretRef:\n            name: s\n        env:\n",
                                     RADIO_PART), "R3")

    def test_radio_pod(self) -> None:
        for old, new in (("automountServiceAccountToken: false", "automountServiceAccountToken: true"),
                         ("runAsNonRoot: true", "runAsNonRoot: false"),
                         ("readOnlyRootFilesystem: true", "readOnlyRootFilesystem: false"),
                         ("type: RuntimeDefault", "type: Unconfined")):
            with self.subTest(old=old):
                self.assertFails(self.broken(old, new, RADIO_PART), "pod")
        self.assertFails(self.broken("          limits:\n            memory: 2Gi\n", "", RADIO_PART), "R5")
        self.assertFails(self.broken("          requests:\n            cpu: 250m\n            memory: 256Mi\n", "",
                                     RADIO_PART), "R5")

    def test_radio_image_is_composes(self) -> None:
        self.assertFails(BASE, "R1", COMPOSE.replace(RADIO_IMAGE, RADIO_IMAGE.replace("0.1.0", "0.1.1")))

    def test_missing_podium(self) -> None:
        self.assertFails(labelled(GOOD) + LOGBOOK_PART + RADIO_PART, "#67")
        self.assertFails(labelled(GOOD) + LOGBOOK_PART + RADIO_PART, "R1")  # compose runs podium

    def test_podium_replicas_exactly_one(self) -> None:
        for count in ("0", "2"):
            with self.subTest(count=count):
                self.assertFails(self.broken("  replicas: 1\n", f"  replicas: {count}\n", PODIUM_PART), "replicas")

    def test_podium(self) -> None:
        for old, new in (("    type: Recreate\n", "    type: RollingUpdate\n"),
                         ("value: nats://nats:4222", "value: nats://localhost:4222"),
                         ("value: :8080", "value: :9090"),
                         ("value: live", "value: replay-x"),
                         ("value: discord", "value: console"),
                         ("        readinessProbe:\n", "        otherProbe:\n"),
                         ("        livenessProbe:\n", "        otherProbe:\n"),
                         ("- containerPort: 8080", "- containerPort: 9090"),
                         ("    port: 8080\n", "    port: 9090\n")):
            with self.subTest(old=old):
                self.assertFails(self.broken(old, new, PODIUM_PART), "#67")

    def test_podium_no_discord_in_the_base(self) -> None:
        # Cards off in the base: no Secret and no channel; Discord is the component's.
        for env in ("        - name: TWR_DISCORD_RESULTS_CHANNEL_ID\n          value: x\n",
                    "        - name: X\n          valueFrom:\n            secretKeyRef:\n              key: k\n              name: s\n"):
            with self.subTest(env=env):
                self.assertFails(self.broken("      - env:\n", "      - env:\n" + env, PODIUM_PART), "#67")

    def test_podium_pod(self) -> None:
        for old, new in (("automountServiceAccountToken: false", "automountServiceAccountToken: true"),
                         ("runAsNonRoot: true", "runAsNonRoot: false"),
                         ("readOnlyRootFilesystem: true", "readOnlyRootFilesystem: false"),
                         ("type: RuntimeDefault", "type: Unconfined")):
            with self.subTest(old=old):
                self.assertFails(self.broken(old, new, PODIUM_PART), "pod")
        self.assertFails(self.broken("          limits:\n            memory: 128Mi\n", "", PODIUM_PART), "R5")

    def test_podium_image_is_composes(self) -> None:
        self.assertFails(BASE, "R1", COMPOSE.replace(PODIUM_IMAGE, PODIUM_IMAGE.replace("0.1.2", "0.1.3")))

    def test_missing_pitwall(self) -> None:
        self.assertFails(labelled(GOOD) + LOGBOOK_PART + RADIO_PART + PODIUM_PART, "#132")
        self.assertFails(labelled(GOOD) + LOGBOOK_PART + RADIO_PART + PODIUM_PART, "R1")  # compose runs it

    def test_pitwall(self) -> None:
        for old, new, rule in (("  replicas: 1\n", "  replicas: 2\n", "replicas"),
                               ("value: nats://nats:4222", "value: nats://localhost:4222", "#132"),
                               ("value: :8080", "value: :9090", "#132"),
                               ("value: live,v1-import", "value: live", "#132"),
                               ("        readinessProbe:\n", "        otherProbe:\n", "#132"),
                               ("        livenessProbe:\n", "        otherProbe:\n", "#132"),
                               ("- containerPort: 8080", "- containerPort: 9090", "#132"),
                               ("    port: 8080\n", "    port: 9090\n", "#132"),
                               ("        securityContext:\n          readOnlyRootFilesystem: true\n",
                                "        securityContext:\n          readOnlyRootFilesystem: true\n"
                                "        volumeMounts:\n        - mountPath: /data\n          name: data\n", "#132"),
                               ("automountServiceAccountToken: false", "automountServiceAccountToken: true", "pod"),
                               ("readOnlyRootFilesystem: true", "readOnlyRootFilesystem: false", "pod"),
                               ("          limits:\n            memory: 256Mi\n", "", "R5")):
            with self.subTest(old=old):
                self.assertFails(self.broken(old, new, PITWALL_PART), rule)

    def test_pitwall_not_exposed_in_the_base(self) -> None:
        self.assertFails(BASE + labelled(PITWALL_INGRESS), "R3")

    def test_pitwall_image_is_composes(self) -> None:
        self.assertFails(BASE, "R1", COMPOSE.replace(PITWALL_IMAGE, PITWALL_IMAGE.replace("0.1.0", "0.1.1")))

    def test_the_base_pins_composes_images(self) -> None:
        """deploy/base/kustomization.yaml's images: block is compose's logbook and
        radio, by tag and digest, offline (make deploy-render checks the render)."""
        text = (ROOT / "deploy" / "base" / "kustomization.yaml").read_text()
        pinned = re.findall(r"^  - name: (\S+)\n    newTag: (\S+)\n    digest: (sha256:[a-f0-9]{64})$", text, re.M)
        self.assertEqual([name for name, _, _ in pinned],
                         ["ghcr.io/those-who-run/twr-logbook", "ghcr.io/those-who-run/twr-radio",
                          "ghcr.io/those-who-run/twr-podium", "ghcr.io/those-who-run/twr-pitwall"])
        compose = (ROOT / "deploy" / "compose" / "compose.yaml").read_text()
        for name, tag, digest in pinned:
            with self.subTest(image=name):
                self.assertIn(f"image: {name}:{tag}@{digest}\n", compose)


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


# radio as deploy/components/radio-discord changes it: the sink, and the two
# settings from the Secret.
DISCORD = RADIO_PART.replace("""        - name: TWR_RADIO_SINK
          value: console
""", """        - name: TWR_RADIO_SINK
          value: discord
        - name: TWR_DISCORD_BOT_TOKEN
          valueFrom:
            secretKeyRef:
              key: TWR_DISCORD_BOT_TOKEN
              name: radio-discord
        - name: TWR_DISCORD_VOICE_CHANNEL_ID
          valueFrom:
            secretKeyRef:
              key: TWR_DISCORD_VOICE_CHANNEL_ID
              name: radio-discord
""")
DISCORD_BASE = labelled(GOOD) + LOGBOOK_PART + DISCORD


class RadioDiscordComponent(unittest.TestCase):
    """deploy/components/radio-discord, rendered over the base (paddock#63 R3)."""

    def check(self, text: str) -> list[str]:
        return deploy_check.check(deploy_check.RADIO_DISCORD, text)

    def assertFails(self, text: str) -> None:
        found = self.check(text)
        self.assertTrue(any(p.startswith("R3:") for p in found), found)

    def test_good(self) -> None:
        self.assertNotEqual(DISCORD, RADIO_PART)
        self.assertEqual(self.check(DISCORD_BASE), [])

    def test_console(self) -> None:
        self.assertFails(BASE)

    def test_broken(self) -> None:
        for old, new in (("          value: discord\n", "          value: console\n"),
                         ("              name: radio-discord\n        - name: TWR_DISCORD_VOICE",
                          "              name: other\n        - name: TWR_DISCORD_VOICE"),
                         ("              key: TWR_DISCORD_VOICE_CHANNEL_ID\n", "              key: CHANNEL\n"),
                         ("          valueFrom:\n            secretKeyRef:\n              key: TWR_DISCORD_BOT_TOKEN\n"
                          "              name: radio-discord\n", "          value: x\n")):
            with self.subTest(old=old):
                self.assertEqual(DISCORD_BASE.count(old), 1, old)
                self.assertFails(DISCORD_BASE.replace(old, new))

    def test_no_secret_in_the_render(self) -> None:
        secret = "---\napiVersion: v1\nkind: Secret\nmetadata:\n  labels:\n    app.kubernetes.io/part-of: twr\n  name: radio-discord\n"
        self.assertFails(DISCORD_BASE + secret)

    def test_missing_radio(self) -> None:
        self.assertFails(labelled(GOOD) + LOGBOOK_PART)

    def test_the_component_is_a_component(self) -> None:
        text = (ROOT / deploy_check.RADIO_DISCORD / "kustomization.yaml").read_text()
        self.assertIn("\nkind: Component\n", text)
        base = (ROOT / deploy_check.BASE / "kustomization.yaml").read_text()
        self.assertNotIn("radio-discord", base.split("resources:", 1)[1].split("labels:", 1)[0])


# podium as deploy/components/podium-discord changes it: the token and the
# results channel from radio's Secret, the channel optional.
PODIUM_DISCORD = PODIUM_PART.replace("""        - name: TWR_PODIUM_SINK
          value: discord
""", """        - name: TWR_PODIUM_SINK
          value: discord
        - name: TWR_DISCORD_BOT_TOKEN
          valueFrom:
            secretKeyRef:
              key: TWR_DISCORD_BOT_TOKEN
              name: radio-discord
        - name: TWR_DISCORD_RESULTS_CHANNEL_ID
          valueFrom:
            secretKeyRef:
              key: TWR_DISCORD_RESULTS_CHANNEL_ID
              name: radio-discord
              optional: true
""")
PODIUM_DISCORD_BASE = labelled(GOOD) + LOGBOOK_PART + RADIO_PART + PODIUM_DISCORD


class PodiumDiscordComponent(unittest.TestCase):
    """deploy/components/podium-discord, rendered over the base (paddock#67)."""

    def check(self, text: str) -> list[str]:
        return deploy_check.check(deploy_check.PODIUM_DISCORD, text)

    def assertFails(self, text: str) -> None:
        found = self.check(text)
        self.assertTrue(any(p.startswith("#67:") for p in found), found)

    def test_good(self) -> None:
        self.assertNotEqual(PODIUM_DISCORD, PODIUM_PART)
        self.assertEqual(self.check(PODIUM_DISCORD_BASE), [])

    def test_cards_off(self) -> None:
        self.assertFails(BASE)

    def test_broken(self) -> None:
        for old, new in (("          value: discord\n", "          value: console\n"),
                         ("              name: radio-discord\n        - name: TWR_DISCORD_RESULTS",
                          "              name: other\n        - name: TWR_DISCORD_RESULTS"),
                         ("              key: TWR_DISCORD_RESULTS_CHANNEL_ID\n", "              key: CHANNEL\n"),
                         ("              optional: true\n", ""),
                         # The token is not optional: without it podium could not post.
                         ("              key: TWR_DISCORD_BOT_TOKEN\n              name: radio-discord\n",
                          "              key: TWR_DISCORD_BOT_TOKEN\n              name: radio-discord\n"
                          "              optional: true\n"),
                         ("          valueFrom:\n            secretKeyRef:\n              key: TWR_DISCORD_BOT_TOKEN\n"
                          "              name: radio-discord\n", "          value: x\n")):
            with self.subTest(old=old):
                self.assertEqual(PODIUM_DISCORD_BASE.count(old), 1, old)
                self.assertFails(PODIUM_DISCORD_BASE.replace(old, new))

    def test_no_secret_in_the_render(self) -> None:
        secret = "---\napiVersion: v1\nkind: Secret\nmetadata:\n  labels:\n    app.kubernetes.io/part-of: twr\n  name: radio-discord\n"
        self.assertFails(PODIUM_DISCORD_BASE + secret)

    def test_missing_podium(self) -> None:
        self.assertFails(labelled(GOOD) + LOGBOOK_PART + RADIO_PART)

    def test_the_component_is_a_component(self) -> None:
        text = (ROOT / deploy_check.PODIUM_DISCORD / "kustomization.yaml").read_text()
        self.assertIn("\nkind: Component\n", text)
        base = (ROOT / deploy_check.BASE / "kustomization.yaml").read_text()
        self.assertNotIn("podium-discord", base.split("resources:", 1)[1].split("labels:", 1)[0])


class PitwallIngressComponent(unittest.TestCase):
    """deploy/components/pitwall-ingress, rendered over the base (paddock#132)."""

    def check(self, text: str) -> list[str]:
        return deploy_check.check(deploy_check.PITWALL_INGRESS, text)

    def assertFails(self, text: str, rule: str = "#132") -> None:
        found = self.check(text)
        self.assertTrue(any(p.startswith(rule + ":") for p in found), found)

    def test_good(self) -> None:
        self.assertEqual(self.check(BASE + labelled(PITWALL_INGRESS)), [])

    def test_label(self) -> None:
        self.assertFails(BASE + PITWALL_INGRESS, "R1")

    def test_no_ingress(self) -> None:
        self.assertFails(BASE)

    def test_nothing_the_overlay_sets(self) -> None:
        for old, new in (("  - http:\n", "  - host: pitwall.example.com\n    http:\n"),
                         ("  rules:\n", "  ingressClassName: my-ingress-class\n  rules:\n"),
                         ("  rules:\n", "  tls:\n  - secretName: pitwall-tls\n  rules:\n")):
            with self.subTest(new=new):
                self.assertFails(BASE + labelled(PITWALL_INGRESS.replace(old, new)))

    def test_backend(self) -> None:
        self.assertFails(BASE + labelled(PITWALL_INGRESS.replace("            name: pitwall\n", "            name: podium\n")))

    def test_the_component_is_a_component(self) -> None:
        text = (ROOT / deploy_check.PITWALL_INGRESS / "kustomization.yaml").read_text()
        self.assertIn("\nkind: Component\n", text)
        base = (ROOT / deploy_check.BASE / "kustomization.yaml").read_text()
        self.assertNotIn("pitwall-ingress", base.split("resources:", 1)[1].split("labels:", 1)[0])


ECU_IMAGE = "ghcr.io/those-who-run/twr-ecu:0.1.15@sha256:" + "c" * 64

# `kustomize build deploy/examples/ecu-live`, with ecu's image as ECU_IMAGE.
ECU_LIVE = f"""\
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  labels:
    app.kubernetes.io/part-of: twr
  name: ecu-recordings
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 5Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/part-of: twr
  name: ecu
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: ecu
  strategy:
    type: Recreate
  template:
    metadata:
      labels:
        app.kubernetes.io/name: ecu
        app.kubernetes.io/part-of: twr
    spec:
      automountServiceAccountToken: false
      containers:
      - env:
        - name: TWR_NATS_URL
          value: nats://nats:4222
        - name: ADDR
          value: :8080
        - name: LOG_LEVEL
          value: info
        - name: TWR_SRC
          value: live
        - name: TWR_ECU_PS5_IP
          value: 192.0.2.10
        - name: TWR_ECU_RECORDINGS_DIR
          value: /recordings
        image: {ECU_IMAGE}
        livenessProbe:
          failureThreshold: 30
          httpGet:
            path: /healthz
            port: http
          periodSeconds: 20
        name: ecu
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 33740
          hostPort: 33740
          name: telemetry
          protocol: UDP
        readinessProbe:
          failureThreshold: 3
          httpGet:
            path: /healthz
            port: http
          periodSeconds: 5
        resources:
          limits:
            memory: 256Mi
          requests:
            cpu: 100m
            memory: 64Mi
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: true
        startupProbe:
          failureThreshold: 24
          httpGet:
            path: /healthz
            port: http
          periodSeconds: 5
        volumeMounts:
        - mountPath: /recordings
          name: recordings
      enableServiceLinks: false
      securityContext:
        fsGroup: 65532
        fsGroupChangePolicy: OnRootMismatch
        runAsGroup: 65532
        runAsNonRoot: true
        runAsUser: 65532
        seccompProfile:
          type: RuntimeDefault
      terminationGracePeriodSeconds: 30
      volumes:
      - name: recordings
        persistentVolumeClaim:
          claimName: ecu-recordings
"""
ECU_COMPOSE = COMPOSE + f"  replay:\n    image: {ECU_IMAGE}\n"


class EcuLive(unittest.TestCase):
    """deploy/examples/ecu-live (paddock#108): the live engineer an overlay copies,
    checked with the base's rules but the host port, and its own."""

    def check(self, text: str, compose: str = ECU_COMPOSE) -> list[str]:
        return deploy_check.check(deploy_check.ECU_LIVE, text, compose)

    def assertFails(self, old: str, new: str, rule: str = "ecu") -> None:
        self.assertEqual(ECU_LIVE.count(old), 1, old)
        found = self.check(ECU_LIVE.replace(old, new))
        self.assertTrue(any(p.startswith(rule + ":") for p in found), found)

    def test_good(self) -> None:
        self.assertEqual(self.check(ECU_LIVE), [])

    def test_the_host_port_is_allowed_only_for_33740(self) -> None:
        # The base's rule still holds for any other host port, here or anywhere else.
        self.assertFails("hostPort: 33740", "hostPort: 33741", "portable")
        self.assertIn(deploy_check.ECU_HOST_PORT, problems(ECU_LIVE, deploy_check.BASE))

    def test_the_host_port_is_allowed_only_once(self) -> None:
        # A second hostPort: 33740 on ecu, or 33740 on another object, fails.
        port = "        - containerPort: 33740\n          hostPort: 33740\n          name: telemetry\n          protocol: UDP\n"
        self.assertEqual(ECU_LIVE.count(port), 1)
        twice = ECU_LIVE.replace(port, port + port.replace("name: telemetry", "name: telemetry-2"))
        self.assertIn(deploy_check.ECU_HOST_PORT, self.check(twice))
        other = ECU_LIVE + (
            "---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  labels:\n"
            "    app.kubernetes.io/part-of: twr\n  name: other\nspec:\n  template:\n    spec:\n"
            f"      containers:\n      - image: {ECU_IMAGE}\n        name: other\n        ports:\n{port}"
            "        resources:\n          requests:\n            cpu: 10m\n")
        self.assertIn("portable: Deployment/other has a host port: hostPort: 33740", self.check(other))
        self.assertNotIn(deploy_check.ECU_HOST_PORT, self.check(other))

    def test_the_claim_is_in_the_render(self) -> None:
        self.assertFails("  name: ecu-recordings\nspec:", "  name: ecu-other\nspec:")

    def test_udp_33740_on_the_host(self) -> None:
        self.assertFails("          hostPort: 33740\n", "")
        self.assertFails("          protocol: UDP\n", "")
        self.assertFails("- containerPort: 33740", "- containerPort: 33741")

    def test_one_replica_by_recreate(self) -> None:
        self.assertFails("  replicas: 1\n", "  replicas: 2\n", "replicas")
        self.assertFails("    type: Recreate\n", "    type: RollingUpdate\n")

    def test_environment(self) -> None:
        for old, new in (("value: 192.0.2.10", "value: 192.0.2.11"),
                         ("value: nats://nats:4222", "value: nats://nats.example.com:4222"),
                         ("value: :8080", "value: :9090"),
                         ("value: live", "value: parity")):
            with self.subTest(new=new):
                self.assertFails(old, new)

    def test_recordings_on_a_kept_volume(self) -> None:
        self.assertFails("value: /recordings", "value: /tmp")
        self.assertFails("        persistentVolumeClaim:\n          claimName: ecu-recordings\n",
                         "        emptyDir: {}\n")

    def test_probes_and_pod(self) -> None:
        self.assertFails("path: /healthz\n            port: http\n          periodSeconds: 20",
                         "path: /metrics\n            port: http\n          periodSeconds: 20")
        self.assertFails("readOnlyRootFilesystem: true", "readOnlyRootFilesystem: false", "pod")
        self.assertFails("      automountServiceAccountToken: false\n", "", "pod")

    def test_label_and_requests(self) -> None:
        self.assertFails("  labels:\n    app.kubernetes.io/part-of: twr\n  name: ecu-recordings\n",
                         "  name: ecu-recordings\n", "R1")
        self.assertFails("          requests:\n            cpu: 100m\n", "          other:\n            cpu: 100m\n",
                         "R5")

    def test_image_is_composes(self) -> None:
        self.assertTrue(any(p.startswith("ecu:") for p in self.check(ECU_LIVE, COMPOSE)))
        self.assertFails(ECU_IMAGE, ECU_IMAGE.replace("0.1.15", "0.1.16"))
        self.assertFails(ECU_IMAGE, "ghcr.io/those-who-run/twr-ecu:0.1.15", "pinned")

    def test_the_real_example_is_composes_and_outside_the_base(self) -> None:
        example = (ROOT / deploy_check.ECU_LIVE / "kustomization.yaml").read_text()
        tag = re.search(r"newTag: (\S+)\n\s+digest: (\S+)", example)
        assert tag is not None
        compose = (ROOT / "deploy" / "compose" / "compose.yaml").read_text()
        self.assertIn(f"ghcr.io/those-who-run/twr-ecu:{tag.group(1)}@{tag.group(2)}", compose)
        self.assertIn(f"value: {deploy_check.PS5_PLACEHOLDER}\n",
                      (ROOT / deploy_check.ECU_LIVE / "ecu-deployment.yaml").read_text())
        base = (ROOT / deploy_check.BASE / "kustomization.yaml").read_text()
        self.assertNotIn("examples", base)


if __name__ == "__main__":
    unittest.main()
