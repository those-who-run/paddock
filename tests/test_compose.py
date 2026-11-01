"""The walking skeleton's compose file (deploy/compose/compose.yaml, paddock#39): every
image pinned by version tag and digest and never `latest` (R2), every port published
on 127.0.0.1 only and no service on the host's network (R3), every service built from
a pinned image rather than `build:` alone, every service with its capabilities
dropped and no new privileges, and the NATS images the same as those the bootstrap is
tested with (scripts/nats_it.sh), so the laptop runs what CI checked.

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
COMPOSE_IT = ROOT / "scripts" / "compose_it.sh"
SOURCES = ROOT / "recordings" / "SOURCES.yaml"

IMAGE = re.compile(r"^\s*image:\s*(\S+)\s*$")
PINNED = re.compile(
    r"^(?P<name>[a-z0-9.-]+(?::\d+)?/[a-z0-9._/-]+)"
    r":(?P<tag>[A-Za-z0-9._-]+)@sha256:[a-f0-9]{64}$"
)


def services(text: str) -> dict[str, list[str]]:
    """Each service's lines under `services:`, by name: a service is a key two spaces
    in, and its lines are those indented deeper, up to the next key at two spaces or
    less."""
    found: dict[str, list[str]] = {}
    current: str | None = None
    inside = False
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        depth = len(line) - len(line.lstrip())
        if depth == 0:
            inside = stripped == "services:"
            current = None
            continue
        if not inside:
            continue
        if depth == 2 and stripped.endswith(":"):
            current = stripped[:-1]
            found[current] = []
        elif current is not None:
            found[current].append(line)
    return found


def keys(lines: list[str]) -> dict[str, str]:
    """A service's own keys (four spaces in) and what follows each on its line."""
    out: dict[str, str] = {}
    for line in lines:
        if len(line) - len(line.lstrip()) == 4 and ":" in line:
            key, _, value = line.strip().partition(":")
            out[key] = value.strip()
    return out


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
        self.assertGreaterEqual(len(found), 6, "nats, nats-bootstrap, logbook, replay, radio, podium, nats-box")
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

    def test_v2_images_from_ghcr(self) -> None:
        # ADR 0006: v2 images are ghcr.io/those-who-run/twr-<repo>.
        for repo in ("logbook", "ecu", "radio", "podium"):
            with self.subTest(repo=repo):
                self.assertTrue(
                    any(i.startswith(f"ghcr.io/those-who-run/twr-{repo}:") for i in images(self.text))
                )

    def test_pinned_rejects(self) -> None:
        digest = "@sha256:" + "a" * 64
        self.assertIsNotNone(PINNED.match("docker.io/library/nats:2.15.1-alpine" + digest))
        self.assertIsNone(PINNED.match("docker.io/library/nats:2.15.1-alpine"))
        self.assertIsNone(PINNED.match("docker.io/library/nats" + digest))
        self.assertIsNone(PINNED.match("nats:2.15.1" + digest))


class Services(unittest.TestCase):
    def setUp(self) -> None:
        self.services = services(COMPOSE.read_text(encoding="utf-8"))

    def test_the_services(self) -> None:
        # R1.
        self.assertEqual(
            sorted(self.services),
            ["logbook", "nats", "nats-bootstrap", "nats-box", "pitwall", "podium", "radio", "replay"],
        )
        self.assertEqual(keys(self.services["nats-box"]).get("profiles"), "[tools]")
        self.assertEqual(keys(self.services["replay"]).get("profiles"), "[replay]")
        self.assertEqual(keys(self.services["radio"]).get("profiles"), "[replay]")
        self.assertEqual(keys(self.services["podium"]).get("profiles"), "[replay]")
        self.assertEqual(keys(self.services["pitwall"]).get("profiles"), "[replay]")

    def test_no_host_network_or_privilege(self) -> None:
        # network_mode: host publishes every port with no ports: entry, past R3's check.
        for name, lines in self.services.items():
            with self.subTest(service=name):
                own = keys(lines)
                for refused in ("network_mode", "privileged", "cap_add", "pid", "ipc"):
                    self.assertNotIn(refused, own)

    def test_image_not_build(self) -> None:
        for name, lines in self.services.items():
            with self.subTest(service=name):
                own = keys(lines)
                self.assertIn("image", own, "a service runs a pinned image, not build: alone")
                self.assertNotIn("build", own)

    def test_hardened(self) -> None:
        for name, lines in self.services.items():
            with self.subTest(service=name):
                own = keys(lines)
                self.assertEqual(own.get("cap_drop"), "[ALL]")
                self.assertEqual(own.get("security_opt"), '["no-new-privileges:true"]')
        for name in ("nats-bootstrap", "logbook", "replay", "radio", "podium", "pitwall"):
            with self.subTest(service=name):
                self.assertEqual(keys(self.services[name]).get("read_only"), "true")

    def test_v2_images_amd64(self) -> None:
        # ADR 0006: amd64 only, so an arm64 laptop runs them under emulation.
        for name in ("logbook", "replay", "radio", "podium", "pitwall"):
            with self.subTest(service=name):
                self.assertEqual(keys(self.services[name]).get("platform"), "linux/amd64")

    def test_replay(self) -> None:
        # What the parity harness's v2 side (parity/v2.go) runs: ecu's own entrypoint,
        # so the command is ecu's arguments, with the server in TWR_NATS_URL; and what
        # `make replay` needs: the recordings read-only at /recordings.
        lines = self.services["replay"]
        own = keys(lines)
        self.assertNotIn("entrypoint", own)
        self.assertIn("ghcr.io/those-who-run/twr-ecu:", own["image"])
        self.assertIn("      TWR_NATS_URL: nats://nats:4222", lines)
        text = "\n".join(lines)
        self.assertRegex(
            text,
            r"source: \$\{TWR_RECORDINGS:-\.\./\.\./recordings\}\n\s+target: /recordings\n\s+read_only: true",
        )

    def test_radio(self) -> None:
        # paddock#63 R1: the console by default, so it needs no Discord, on what
        # `make replay` publishes, with the WAV directory mounted for the wav sink;
        # no Discord setting and no env file.
        lines = self.services["radio"]
        own = keys(lines)
        self.assertNotIn("entrypoint", own)
        self.assertNotIn("env_file", own)
        self.assertNotIn("ports", own)
        self.assertEqual(own.get("tmpfs"), "[/tmp]")
        self.assertIn("      TWR_NATS_URL: nats://nats:4222", lines)
        self.assertIn("      TWR_RADIO_SINK: ${TWR_RADIO_SINK:-console}", lines)
        self.assertIn("      TWR_RADIO_SRC: ${TWR_RADIO_SRC:-replay-compose}", lines)
        self.assertIn("      TWR_RADIO_WAV_DIR: /wav", lines)
        self.assertFalse(any("DISCORD" in line for line in lines))
        self.assertRegex("\n".join(lines), r"source: \$\{TWR_RADIO_WAV:-[^}]+\}\n\s+target: /wav\n")

    def test_podium(self) -> None:
        # paddock#67: the console sink, so it needs no Discord, on what `make
        # replay` publishes; no Discord setting, no env file and no port.
        lines = self.services["podium"]
        own = keys(lines)
        self.assertNotIn("entrypoint", own)
        self.assertNotIn("env_file", own)
        self.assertNotIn("ports", own)
        self.assertIn("      TWR_NATS_URL: nats://nats:4222", lines)
        self.assertIn("      TWR_PODIUM_SINK: console", lines)
        self.assertIn("      TWR_PODIUM_SRC: ${TWR_PODIUM_SRC:-replay-compose}", lines)
        self.assertFalse(any("DISCORD" in line for line in lines))

    def test_pitwall(self) -> None:
        # paddock#132 R1: the replays' sessions by default, from logbook over NATS, at
        # v1's port 9852 on loopback; no volume, since it holds no data.
        lines = self.services["pitwall"]
        own = keys(lines)
        self.assertNotIn("entrypoint", own)
        self.assertNotIn("volumes", own)
        self.assertIn("ghcr.io/those-who-run/twr-pitwall:", own["image"])
        self.assertIn("      TWR_NATS_URL: nats://nats:4222", lines)
        self.assertIn("      TWR_PITWALL_SRC: ${TWR_PITWALL_SRC:-replay-compose}", lines)
        self.assertIn('      - "127.0.0.1:9852:8080"', lines)
        # R1: NATS and logbook as dependencies.
        self.assertRegex("\n".join(lines), r"depends_on:\n(.*\n)*\s+logbook:\n\s+condition: service_started")

    def test_parser(self) -> None:
        text = (
            "name: x\nservices:\n  a:\n    image: i\n    network_mode: host\n"
            "    environment:\n      K: v\n  b:\n    build: .\nvolumes:\n  v:\n"
        )
        found = services(text)
        self.assertEqual(sorted(found), ["a", "b"])
        self.assertEqual(keys(found["a"]), {"image": "i", "network_mode": "host", "environment": ""})
        self.assertEqual(keys(found["b"]), {"build": "."})


class Ports(unittest.TestCase):
    def test_published_on_loopback_only(self) -> None:
        found = ports(COMPOSE.read_text(encoding="utf-8"))
        self.assertEqual(
            sorted(found),
            sorted(["127.0.0.1:4222:4222", "127.0.0.1:8222:8222", "127.0.0.1:8080:8080", "127.0.0.1:9852:8080"]),
        )
        for port in found:
            with self.subTest(port=port):
                self.assertTrue(port.startswith("127.0.0.1:"))

    def test_ports_parser(self) -> None:
        text = "a:\n  ports:\n    - \"0.0.0.0:1:1\"\n    - 2:2\n  volumes:\n    - x:/y\n"
        self.assertEqual(ports(text), ["0.0.0.0:1:1", "2:2"])


class Targets(unittest.TestCase):
    def test_make_targets(self) -> None:
        # R4.
        text = MAKEFILE.read_text(encoding="utf-8")
        for target in ("up", "replay", "radio", "podium", "pitwall", "sub", "export", "down", "reset", "compose-it"):
            with self.subTest(target=target):
                self.assertRegex(text, rf"(?m)^{re.escape(target)}:")

    def test_compose_it_replays_a_listed_recording(self) -> None:
        # AC2 replays a recording that is in recordings/ with its provenance.
        m = re.search(r"(?m)^recording=(\S+)$", COMPOSE_IT.read_text(encoding="utf-8"))
        self.assertIsNotNone(m)
        assert m is not None
        self.assertIn(f"  - file: {m.group(1)}\n", SOURCES.read_text(encoding="utf-8"))
        self.assertTrue((ROOT / "recordings" / m.group(1)).is_file())


if __name__ == "__main__":
    unittest.main()
