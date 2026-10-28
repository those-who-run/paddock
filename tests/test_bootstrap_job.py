"""The stream and KV bootstrap Job (deploy/nats/kustomization.yaml and
bootstrap-job.yaml, paddock#43).

A ConfigMap generator lists its files, so a new definition file left out of it would
never be mounted and the Job would pass without creating it: every file in
deploy/nats/streams and deploy/nats/kv is in its directory's generator, and nothing
else is. The Job runs the image compose runs the bootstrap in, as a non-root user on
a read-only root file system with no service account token, waits 5 min for
JetStream, reruns on every Argo CD sync and deletes itself 10 min after it finishes.

Standard library only, as paddock's other checks are, so the files are read line by
line: kustomize's YAML here is block style, one key a line."""

from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
NATS = ROOT / "deploy" / "nats"
KUSTOMIZATION = NATS / "kustomization.yaml"
JOB = NATS / "bootstrap-job.yaml"
COMPOSE = ROOT / "deploy" / "compose" / "compose.yaml"


def generators(text: str) -> dict[str, list[str]]:
    """Each configMapGenerator entry's files, by name."""
    found: dict[str, list[str]] = {}
    current: str | None = None
    inside = False
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if not line.startswith(" "):
            inside = line.startswith("configMapGenerator:")
            continue
        if not inside:
            continue
        if m := re.match(r"^  - name: (\S+)$", line):
            current = m.group(1)
            found[current] = []
        elif current and (m := re.match(r"^      - (\S+)$", line)):
            found[current].append(m.group(1))
    return found


def has(text: str, line: str) -> bool:
    return re.search(rf"^\s*{re.escape(line)}\s*$", text, flags=re.M) is not None


class Generators(unittest.TestCase):
    def setUp(self) -> None:
        self.gens = generators(KUSTOMIZATION.read_text())

    def test_one_configmap_a_directory(self) -> None:
        for name, directory in (("twr-nats-streams", "streams"), ("twr-nats-kv", "kv")):
            with self.subTest(directory=directory):
                on_disk = sorted(f"{directory}/{p.name}" for p in (NATS / directory).glob("*.json"))
                self.assertTrue(on_disk)
                self.assertEqual(sorted(self.gens.get(name, [])), on_disk,
                                 f"{name} must list every file in deploy/nats/{directory}, and only those")

    def test_the_script(self) -> None:
        self.assertEqual(self.gens.get("twr-nats-bootstrap-script"), ["bootstrap.sh"])

    def test_parser_sees_a_missing_file(self) -> None:
        text = KUSTOMIZATION.read_text().replace("      - streams/telemetry.json\n", "")
        self.assertNotIn("streams/telemetry.json", generators(text)["twr-nats-streams"])


class Job(unittest.TestCase):
    def setUp(self) -> None:
        self.job = JOB.read_text()

    def test_name_and_reruns(self) -> None:
        for line in ("name: twr-nats-bootstrap",
                     "ttlSecondsAfterFinished: 600",
                     "argocd.argoproj.io/hook: Sync",
                     "argocd.argoproj.io/hook-delete-policy: BeforeHookCreation"):
            with self.subTest(line=line):
                self.assertTrue(has(self.job, line))

    def test_hardened(self) -> None:
        for line in ("automountServiceAccountToken: false",
                     "runAsNonRoot: true",
                     "runAsUser: 1000",
                     "readOnlyRootFilesystem: true",
                     "allowPrivilegeEscalation: false",
                     "drop: [ALL]",
                     "restartPolicy: Never"):
            with self.subTest(line=line):
                self.assertTrue(has(self.job, line))

    def test_nats_url_and_wait(self) -> None:
        # The chart's client Service (deploy/nats/chart, release `nats`).
        self.assertRegex(self.job, r"- name: NATS_URL\n\s+value: nats://nats:4222\n")
        self.assertRegex(self.job, r'- name: WAIT_SECONDS\n\s+value: "300"\n')
        self.assertIn("exec sh /bootstrap/bootstrap.sh /nats", self.job)

    def test_mounts(self) -> None:
        for path, cm in (("/bootstrap", "twr-nats-bootstrap-script"),
                         ("/nats/streams", "twr-nats-streams"),
                         ("/nats/kv", "twr-nats-kv")):
            with self.subTest(path=path):
                self.assertTrue(has(self.job, f"mountPath: {path}"))
                self.assertTrue(has(self.job, f"name: {cm}"))

    def test_image_is_composes_bootstrap(self) -> None:
        images = re.findall(r"^\s*image: (\S+)$", self.job, flags=re.M)
        self.assertEqual(len(images), 1)
        self.assertRegex(images[0], r"^docker\.io/natsio/nats-box:[\w.-]+@sha256:[a-f0-9]{64}$")
        compose = COMPOSE.read_text()
        bootstrap = compose[compose.index("  nats-bootstrap:"):compose.index("  logbook:")]
        self.assertIn(f"image: {images[0]}\n", bootstrap)


if __name__ == "__main__":
    unittest.main()
