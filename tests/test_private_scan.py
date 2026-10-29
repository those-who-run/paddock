"""The private-detail scan (scripts/private_scan.py, paddock#50): no address, host
name or stream definition that belongs to one cluster, in any file.

The scan reads this file too, so every value that must fail is built from its
parts (PRIVATE_IP, dot()) and never appears whole. `make check-deploy` runs
the scan on the repository itself."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import subprocess
import tempfile
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
N = "nats"
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
            "nats://nats.twr.svc.cluster.local:4222 logbook.twr.svc.cluster.local http://localhost:8080\n"
        )
        self.assertEqual(scan(text), [])

    def test_mdns_local_is_not_a_placeholder(self) -> None:
        self.assertEqual(len(scan(dot("printer", "local") + "\n")), 1)
        self.assertEqual(len(scan("http://" + dot("nuc", "local") + ":8080\n")), 1)

    def test_wildcard_and_leading_dot_hosts_fail(self) -> None:
        for line in (
            "*." + dot("home", "lan"),
            "NO_PROXY=." + dot("home", "lan"),
            "domain ." + dot("pitwall", "home", "lan"),
            '["*.' + dot("nuc", "lab") + '"]',
        ):
            with self.subTest(line=line):
                self.assertEqual(len(scan(line + "\n")), 1)

    def test_home_network_suffixes_fail(self) -> None:
        for tld in ("box", "lab", "homelab", "private", "lan", "internal"):
            with self.subTest(tld=tld):
                self.assertEqual(len(scan("ssh " + dot("nuc", tld) + "\n")), 1)

    def test_code_ending_like_a_tld_passes(self) -> None:
        self.assertEqual(scan("metadata.labels.app spec.site page.info self.me Path.home() x.build\n"), [])

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
    # The scan reads Python too, so each command is built with N and never appears
    # whole in this file.

    def test_a_stream_add_in_a_script_fails(self) -> None:
        problems = scan(f"#!/bin/sh\n{N} --server x stream add --config s.json\n", "scripts/x.sh")
        self.assertEqual(len(problems), 1)
        self.assertTrue(problems[0].startswith("bootstrap: scripts/x.sh:2: "), problems)

    def test_each_form_fails(self) -> None:
        for line in ("kv add live", "stream edit EVENTS", "str create X", "kv new b", "object add files", "obj add f"):
            with self.subTest(line=line):
                self.assertEqual(len(scan(f"{N} {line}\n", "Makefile")), 1)

    def test_a_yaml_flow_command_fails(self) -> None:
        text = f'command: ["{N}", "stream", "add", "--config", "/s.json"]\n'
        self.assertEqual(len(scan(text, "deploy/base/job.yaml")), 1)

    def test_a_yaml_block_list_fails(self) -> None:
        text = f"spec:\n  args:\n    - {N}\n    - --server\n    - nats://nats:4222\n    - stream\n    - add\n  x: y\n"
        problems = scan(text, "deploy/base/job.yaml")
        self.assertEqual(len(problems), 1, problems)
        self.assertTrue(problems[0].startswith("bootstrap: deploy/base/job.yaml:3: "), problems)

    def test_a_yaml_list_of_other_items_passes(self) -> None:
        text = f"a:\n  - {N}\n  - stream\nb:\n  - add\n"
        self.assertEqual(scan(text, "x.yaml"), [])

    def test_a_continued_line_fails(self) -> None:
        text = f"run:\n\t{N} --server x \\\n\t\tkv \\\n\t\tadd live\n"
        problems = scan(text, "Makefile")
        self.assertEqual(len(problems), 1, problems)
        self.assertTrue(problems[0].startswith("bootstrap: Makefile:2: "), problems)

    def test_python_fails(self) -> None:
        text = f'subprocess.run(["{N}", "kv", "add", "live"])\n'
        self.assertEqual(len(scan(text, "scripts/x.py")), 1)

    def test_a_markdown_code_block_fails_and_prose_passes(self) -> None:
        text = f"bootstrap.sh runs `{N} stream add --config`.\n\n```sh\n{N} kv add live\n```\n"
        problems = scan(text, "deploy/README.md")
        self.assertEqual(len(problems), 1)
        self.assertTrue(problems[0].startswith("bootstrap: deploy/README.md:4: "), problems)

    def test_comments_and_data_pass(self) -> None:
        self.assertEqual(scan(f"# the fields of {N} kv add\n", "deploy/x.sh"), [])
        self.assertEqual(scan(f"# the fields of {N} kv add\n", "scripts/x.py"), [])
        self.assertEqual(scan(f'{{"why": "{N} kv add fixes it"}}\n', "deploy/nats/kv/x.json"), [])

    def test_bootstrap_sh_may(self) -> None:
        self.assertEqual(scan(f"{N} stream add --config s.json\n", private_scan.BOOTSTRAP), [])

    def test_reads_pass(self) -> None:
        self.assertEqual(scan(f"{N} stream info EVENTS\n{N} kv get live x\n", "x.sh"), [])


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


class Main(unittest.TestCase):
    def run_main(self, allow: str, files: dict[str, str]) -> tuple[int, str]:
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / "scripts").mkdir()
            (root / private_scan.ALLOW).write_text(allow)
            for name, text in files.items():
                (root / name).write_text(text)
            subprocess.run(["git", "init", "-q", d], check=True)
            err = io.StringIO()
            with contextlib.redirect_stderr(err), contextlib.redirect_stdout(io.StringIO()):
                code = private_scan.main(root)
            return code, err.getvalue()

    def test_a_used_allowlist_passes(self) -> None:
        code, err = self.run_main(ALLOW, {"a.md": "https://github.com/x networking.k8s.io/v1\n"})
        self.assertEqual((code, err), (0, ""))

    def test_an_unused_entry_fails(self) -> None:
        # The allowlist names k8s.io itself; that must not count as a use.
        code, err = self.run_main(ALLOW, {"a.md": "https://github.com/x\n"})
        self.assertEqual(code, 1)
        self.assertIn("allowlist: scripts/private_scan.allow:2: k8s.io is used by no file", err)

    def test_a_private_value_fails(self) -> None:
        code, err = self.run_main(ALLOW, {"a.md": f"github.com k8s.io {PRIVATE_IP}\n"})
        self.assertEqual(code, 1)
        self.assertIn(f"address: a.md:1: {PRIVATE_IP}", err)


if __name__ == "__main__":
    unittest.main()
