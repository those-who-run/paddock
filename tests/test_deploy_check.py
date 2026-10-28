"""The render check (scripts/deploy_check.py, paddock#42): a public base names no
namespace, storage class, node or host port and pins every image, and NATS's chart
renders one server with JetStream on file storage, 4222 and the WebSocket in the
cluster only, and a WebSocket client that may only subscribe (R6).

GOOD is a cut-down `kustomize build --enable-helm deploy/nats/chart`, in the style
kustomize writes; each test breaks one thing in it. `make deploy-render` runs the
check on the real render."""

from __future__ import annotations

import contextlib
import importlib.util
import io
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
                  "twr.>",
                  "_INBOX.>"
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
      containers:
      - image: {BOX}
        name: nats-box
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nats
spec:
  replicas: 1
  template:
    spec:
      containers:
      - env:
        - name: NATS_JS_MAX_FILE_STORE
          value: 5Gi
        image: docker.io/library/nats:2.15.1-alpine@sha256:{"a" * 64}
        name: nats
      - image: docker.io/natsio/prometheus-nats-exporter:0.20.2@sha256:{"b" * 64}
        name: prom-exporter
  volumeClaimTemplates:
  - metadata:
      name: nats-js
    spec:
      resources:
        requests:
          storage: 5Gi
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
             "Deployment/nats-box", "StatefulSet/nats", "Ingress/nats-ws"],
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
        self.assertEqual(problems("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n", "deploy/base"), [])

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
        self.assertFails(broken("  - http:\n", "  - host: nats.example.com\n    http:\n"), "R3")
        self.assertFails(broken("  rules:\n", "  ingressClassName: my-ingress-class\n  rules:\n"), "R3")
        self.assertFails(broken("              name: websocket", "              name: nats"), "R3")

    def test_r4(self) -> None:
        self.assertFails(broken("        name: prom-exporter", "        name: other"), "R4")

    def test_r6(self) -> None:
        self.assertFails(broken('"no_auth_user": "ws"', '"no_auth_user": "twr"'), "R6")
        self.assertFails(broken('"STANDARD"', '"STANDARD", "WEBSOCKET"'), "R6")
        self.assertFails(broken('"deny": [\n                  ">"\n                ]', '"allow": [">"]'), "R6")
        self.assertFails(broken('"_INBOX.>"', '"_INBOX.>", "$JS.API.>"'), "R6")
        self.assertFails(broken('"user": "twr"', '"user": "twr", "password": "x"'), "R6")


def in_namespace(text: str, namespace: str) -> str:
    """GOOD as an overlay with `namespace:` renders it: each object's metadata names it."""
    return text.replace("metadata:\n", f"metadata:\n  namespace: {namespace}\n")


class NamespaceProbe(unittest.TestCase):
    """The overlay probe (kustomize#6058): every namespaced object takes the overlay's
    namespace."""

    def check(self, text: str) -> list[str]:
        return deploy_check.namespaced(deploy_check.documents(text), "probe")

    def test_every_object_in_the_namespace(self) -> None:
        self.assertEqual(self.check(in_namespace(GOOD, "probe")), [])

    def test_objects_left_out(self) -> None:
        # What kustomize 5.8 renders: the overlay's namespace on the Ingress only.
        text = in_namespace(GOOD, "probe").replace("  namespace: probe\n", "", 5)
        found = self.check(text)
        self.assertEqual(len(found), 5, found)
        self.assertTrue(all(p.startswith("namespace: ") for p in found))
        self.assertIn("ConfigMap/nats-config", found[0])

    def test_another_namespace(self) -> None:
        self.assertEqual(len(self.check(in_namespace(GOOD, "other"))), 6)

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


if __name__ == "__main__":
    unittest.main()
