"""The walking skeleton's compose file (deploy/compose/compose.yaml, paddock#39): every
image pinned by version tag and digest and never `latest` (R2), every port published
on 127.0.0.1 only (R3), and the NATS images the same as those the bootstrap is tested
with (scripts/nats_it.sh), so the laptop runs what CI checked.

Standard library only, as paddock's other checks are, so the file is read line by
line: an `image:` line and a quoted `"host:port:port"` item under `ports:`."""

from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
COMPOSE = ROOT / "deploy" / "compose" / "compose.yaml"
NATS_IT = ROOT / "scripts" / "nats_it.sh"
MAKEFILE = ROOT / "Makefile"

IMAGE = re.compile(r"^\s*image:\s*(\S+)\s*$")
PINNED = re.compile(
    r"^(?P<name>[a-z0-9.-]+(?::\d+)?/[a-z0-9._/-]+)"
    r":(?P<tag>[A-Za-z0-9._-]+)@sha256:[a-f0-9]{64}$"
)


def images(text: str) -> list[str]:
    return [m.group(1) for line in text.splitlines() if (m := IMAGE.match(line))]


def ports(text: str) -> list[str]:
    """The items of every `ports:` list."""
    found: list[str] = []
    indent: int | None = None
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        depth = len(line) - len(line.lstrip())
        if stripped == "ports:":
            indent = depth
            continue
        if indent is not None:
            if depth > indent and stripped.startswith("- "):
                found.append(stripped[2:].strip().strip("\"'"))
                continue
            indent = None
    return found


class Images(unittest.TestCase):
    def setUp(self) -> None:
        self.text = COMPOSE.read_text(encoding="utf-8")

    def test_every_image_pinned_by_tag_and_digest(self) -> None:
        found = images(self.text)
        self.assertGreaterEqual(len(found), 4, "nats, nats-bootstrap, logbook, nats-box")
        for image in found:
            with self.subTest(image=image):
                m = PINNED.match(image)
                self.assertIsNotNone(m, f"{image} is not registry/name:tag@sha256:<digest>")
                assert m is not None
                self.assertNotEqual(m.group("tag"), "latest")

    def test_nats_images_match_nats_it(self) -> None:
        tested = set(re.findall(r"^NATS_\w+=(\S+)$", NATS_IT.read_text(encoding="utf-8"), re.M))
        self.assertEqual(len(tested), 2, "nats_it.sh pins nats and nats-box")
        nats = {i for i in images(self.text) if i.startswith(("docker.io/library/nats:", "docker.io/natsio/"))}
        self.assertEqual(nats, tested)

    def test_logbook_from_ghcr(self) -> None:
        # ADR 0006: v2 images are ghcr.io/those-who-run/twr-<repo>.
        self.assertTrue(
            any(i.startswith("ghcr.io/those-who-run/twr-logbook:") for i in images(self.text))
        )

    def test_pinned_rejects(self) -> None:
        digest = "@sha256:" + "a" * 64
        self.assertIsNotNone(PINNED.match("docker.io/library/nats:2.15.1-alpine" + digest))
        self.assertIsNone(PINNED.match("docker.io/library/nats:2.15.1-alpine"))
        self.assertIsNone(PINNED.match("docker.io/library/nats" + digest))
        self.assertIsNone(PINNED.match("nats:2.15.1" + digest))


class Ports(unittest.TestCase):
    def test_published_on_loopback_only(self) -> None:
        found = ports(COMPOSE.read_text(encoding="utf-8"))
        self.assertEqual(
            sorted(found),
            sorted(["127.0.0.1:4222:4222", "127.0.0.1:8222:8222", "127.0.0.1:8080:8080"]),
        )
        for port in found:
            with self.subTest(port=port):
                self.assertTrue(port.startswith("127.0.0.1:"))

    def test_ports_parser(self) -> None:
        text = "a:\n  ports:\n    - \"0.0.0.0:1:1\"\n    - 2:2\n  volumes:\n    - x:/y\n"
        self.assertEqual(ports(text), ["0.0.0.0:1:1", "2:2"])


class Targets(unittest.TestCase):
    def test_make_targets(self) -> None:
        # R4: replay comes with ecu's replay (paddock#35).
        text = MAKEFILE.read_text(encoding="utf-8")
        for target in ("up", "sub", "export", "down", "reset", "compose-it"):
            with self.subTest(target=target):
                self.assertRegex(text, rf"(?m)^{re.escape(target)}:")


if __name__ == "__main__":
    unittest.main()
