"""The private-detail scan (scripts/private_scan.py, paddock#50): no address, host
name or stream definition that belongs to one cluster, in any file.

The scan reads this file too, so every value that must fail is built from its
parts (PRIVATE_IP, dot()) and never appears whole. `make check-deploy` runs
the scan on the repository itself."""

from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

_spec = importlib.util.spec_from_file_location("private_scan", ROOT / "scripts" / "private_scan.py")
assert _spec is not None and _spec.loader is not None
private_scan = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(private_scan)

PRIVATE_IP = "10.20.30." + "40"


def dot(*labels: str) -> str:
    return ".".join(labels)


PRIVATE_HOST = dot("nuc", "home", "arpa")
ALLOW = "github.com  The org's code host.\nk8s.io  Kubernetes API groups.\n"


def allowlist() -> private_scan.Allowlist:
    allow, problems = private_scan.read_allowlist(ALLOW)
    assert not problems, problems
    return allow


def scan(text: str, path: str = "docs/x.md") -> list[str]:
    return private_scan.scan(path, text, allowlist())


class Addresses(unittest.TestCase):
    def test_a_private_address_fails_with_rule_file_and_line(self) -> None:
        problems = scan(f"one\nthe NUC is at {PRIVATE_IP}:4222\n", "deploy/README.md")
        self.assertEqual(len(problems), 1)
        self.assertTrue(problems[0].startswith(f"address: deploy/README.md:2: {PRIVATE_IP} "), problems)

    def test_any_address_outside_the_ranges_fails(self) -> None:
        for ip in ("192.168.1." + "2", "172.16.0." + "9", "8.8.8." + "8", "192.0.3." + "1"):
            with self.subTest(ip=ip):
                self.assertEqual(len(scan(f"addr {ip}\n")), 1)

    def test_an_address_after_an_escape_fails(self) -> None:
        self.assertEqual(len(scan('"a\\n' + PRIVATE_IP + '"\n')), 1)

    def test_documentation_loopback_and_unspecified_pass(self) -> None:
        text = "192.0.2.10 198.51.100.7/24 203.0.113.255 127.0.0.1 127.1.2.3 0.0.0.0:4222\n"
        self.assertEqual(scan(text), [])

    def test_versions_and_numbers_that_are_not_addresses_pass(self) -> None:
        self.assertEqual(scan("rev v0.11.0.1, 1.2.3.4.5, 300.1.2.3, oid 1.3.6.1.4.1\n"), [])


class Hosts(unittest.TestCase):
    def test_a_bare_host_fails(self) -> None:
        problems = scan(f"ssh {PRIVATE_HOST}\n", "deploy/x.yaml")
        self.assertEqual(len(problems), 1)
        self.assertTrue(problems[0].startswith(f"host: deploy/x.yaml:1: {PRIVATE_HOST} "), problems)

    def test_a_url_host_fails_with_user_and_port(self) -> None:
        problems = scan(f"url: nats://twr:secret@{PRIVATE_HOST}:4222\n")
        self.assertEqual(len(problems), 1, problems)
        self.assertIn(PRIVATE_HOST, problems[0])

    def test_a_url_host_with_any_tld_fails(self) -> None:
        # A URL's host fails whatever its top-level domain, even one HOST_TLDS leaves out.
        problems = scan("see https://" + dot("pitwall", "my-lab", "sh") + "/x\n")
        self.assertEqual(len(problems), 1, problems)

    def test_an_email_domain_fails(self) -> None:
        self.assertEqual(len(scan("mail me@" + "someone." + "ie\n")), 1)

    def test_placeholders_pass(self) -> None:
        text = (
            "host: nats.example.com\nhttps://example.com/x a.example b.example.org c.test d.invalid\n"
            "nats://nats.twr.svc.cluster.local:4222 printer.local http://localhost:8080\n"
        )
        self.assertEqual(scan(text), [])

    def test_names_with_no_dot_pass(self) -> None:
        self.assertEqual(scan("TWR_NATS_URL=nats://nats:4222 http://logbook:8080 nats://user:pass@host:4222\n"), [])

    def test_the_allowlist_allows_a_host_and_the_names_under_it(self) -> None:
        text = "https://github.com/those-who-run x@users.noreply.github.com networking.k8s.io/v1\n"
        self.assertEqual(scan(text), [])

    def test_a_name_that_only_ends_like_an_allowed_host_fails(self) -> None:
        self.assertEqual(len(scan("https://" + dot("notgithub", "com") + "/x\n")), 1)

    def test_file_names_code_and_subjects_pass(self) -> None:
        text = "compose.yaml bootstrap.sh deploy_check.py README.md sys.argv twr.v1.*.frame.gt7.lo\n"
        self.assertEqual(scan(text), [])

    def test_an_escape_is_a_separator(self) -> None:
        self.assertEqual(scan('"a\\nk8s.io\\tgithub.com"\n'), [])
        self.assertEqual(len(scan('"a\\n' + PRIVATE_HOST + '"\n')), 1)

    def test_templates_pass(self) -> None:
        self.assertEqual(scan("https://${HOST}/x nats://$host:4222\n"), [])


class Bootstrap(unittest.TestCase):
    def test_a_stream_add_in_a_script_fails(self) -> None:
        problems = scan("#!/bin/sh\nnats --server x stream add --config s.json\n", "scripts/x.sh")
        self.assertEqual(len(problems), 1)
        self.assertTrue(problems[0].startswith("bootstrap: scripts/x.sh:2: "), problems)

    def test_each_form_fails(self) -> None:
        for line in ("nats kv add live", "nats stream edit EVENTS", "nats str create X", "nats kv new b"):
            with self.subTest(line=line):
                self.assertEqual(len(scan(line + "\n", "Makefile")), 1)

    def test_a_yaml_command_fails(self) -> None:
        text = 'command: ["nats", "stream", "add", "--config", "/s.json"]\n'
        self.assertEqual(len(scan(text, "deploy/base/job.yaml")), 1)

    def test_a_markdown_code_block_fails_and_prose_passes(self) -> None:
        text = "bootstrap.sh runs `nats stream add --config`.\n\n```sh\nnats kv add live\n```\n"
        problems = scan(text, "deploy/README.md")
        self.assertEqual(len(problems), 1)
        self.assertTrue(problems[0].startswith("bootstrap: deploy/README.md:4: "), problems)

    def test_comments_and_data_pass(self) -> None:
        self.assertEqual(scan("# the fields of nats kv add\n", "deploy/x.sh"), [])
        self.assertEqual(scan('{"why": "nats kv add fixes it"}\n', "deploy/nats/kv/x.json"), [])

    def test_bootstrap_sh_may(self) -> None:
        self.assertEqual(scan("nats stream add --config s.json\n", private_scan.BOOTSTRAP), [])

    def test_reads_pass(self) -> None:
        self.assertEqual(scan("nats stream info EVENTS\nnats kv get live x\n", "x.sh"), [])


class Allowlist(unittest.TestCase):
    def test_an_entry_without_a_reason_fails(self) -> None:
        _, problems = private_scan.read_allowlist("# c\n\ngithub.com\n")
        self.assertEqual(problems, ["allowlist: scripts/private_scan.allow:3: github.com has no reason"])

    def test_a_duplicate_or_a_non_host_fails(self) -> None:
        _, problems = private_scan.read_allowlist(f"{dot('a', 'com')} why\n{dot('a', 'com')} why\nlocalhost why\n")
        self.assertEqual(len(problems), 2, problems)

    def test_used_entries_are_recorded(self) -> None:
        allow = allowlist()
        scan_ = private_scan.scan("x.md", "https://github.com/x\n", allow)
        self.assertEqual(scan_, [])
        self.assertEqual(allow.used, {"github.com"})


if __name__ == "__main__":
    unittest.main()
