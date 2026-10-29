"""Scan every file in paddock for private details (paddock#50, R3 and R4).

paddock is public, and nothing about one cluster belongs in it: the maintainer's
own deployment lives in a private overlay (ADR 0004, point 8; AGENTS.md, "Never in
a repository"). This reads every text file git knows (tracked, and new files git
does not ignore), Markdown, docs and ADRs included, line by line, for:

- ``address``: an IPv4 address outside the documentation ranges (192.0.2.0/24,
  198.51.100.0/24, 203.0.113.0/24), loopback and 0.0.0.0;
- ``host``: a host name that is not a placeholder (``example.com``, ``*.example``,
  the other names RFC 2606 and RFC 6761 reserve, and ``cluster.local`` for
  in-cluster names), not a name with no dot (an in-cluster Service,
  ``localhost``), and not in the allowlist, scripts/private_scan.allow, where each
  public host has a reason. ``.local`` is mDNS, a home network's own names, so it
  is not a placeholder;
- ``bootstrap``: a ``nats stream``, ``nats kv`` or ``nats object`` command that adds
  or edits a stream or bucket anywhere but deploy/nats/bootstrap.sh, which is the
  one place they are defined. Only commands are read: shell, Makefiles, YAML,
  Python's argument lists and Markdown's code blocks, not their comments or prose, so a page may say
  what bootstrap.sh runs. A command split over lines with a backslash, or written
  as a YAML block list (a Job's ``args:``), is read as one line.

A host name is found two ways: the host of any URL (``scheme://[user@]host``), and
any other dotted name that ends in a top-level domain from HOST_TLDS, after a
wildcard's ``*.`` or a leading dot (a NO_PROXY entry) too. A dotted
name ending in anything else (``compose.yaml``, ``sys.argv``, a NATS subject) is not
a host to this scan; HOST_TLDS leaves out the top-level domains that are also file
extensions or code here (``.sh``, ``.py``, ``.md``, ``.app``, ``.build``).

Go's go.mod, go.sum, go.work and go.work.sum are not read: they list module paths,
not hosts, and ``go mod`` writes them.

Each failure names the rule, the file and the line. An allowlist entry no line uses
fails too, so the list stays short. Standard library only, as paddock's other
checks are.

    python3 scripts/private_scan.py
"""

from __future__ import annotations

import ipaddress
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
# Go's module files list module paths, not hosts a deployment reaches, and `go mod`
# writes them, so they are not scanned.
SKIPPED_NAMES = {"go.mod", "go.sum", "go.work", "go.work.sum"}
ALLOW = Path("scripts/private_scan.allow")
BOOTSTRAP = "deploy/nats/bootstrap.sh"

# The documentation ranges (RFC 5737) and loopback name no real machine; 0.0.0.0 is
# "every interface" in a bind address.
ALLOWED_NETWORKS = tuple(
    ipaddress.ip_network(n) for n in ("192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "127.0.0.0/8", "0.0.0.0/32")
)
# Four dot-separated numbers, not part of a longer dotted run (a version such as
# v0.11.0.1 or 1.2.3.4.5).
IPV4 = re.compile(r"(?<![\w.])(\d{1,3}(?:\.\d{1,3}){3})(?![\w]|\.\d)")

# Names that are reserved and can never be a real host: RFC 2606's example.* and
# .example, .invalid and .test; RFC 6761's localhost; and cluster.local, the
# in-cluster names (svc.cluster.local). Not .local: mDNS gives a home network's
# machines their real names there.
PLACEHOLDER_SUFFIXES = (
    "example.com", "example.net", "example.org", "example", "invalid", "test", "localhost", "cluster.local",
)

# The top-level domains a bare dotted name is read as a host for: the common generic
# ones, the country codes most likely here, and the suffixes home networks use. Not
# .sh, .py, .md or .go, which are file names here far more often than hosts, nor
# .app, .page, .site, .build, .me or .info, which end code (metadata.labels.app)
# as often. A name followed by ( is a call, not a host. A URL's host is read whatever its top-level domain.
HOST_TLDS = (
    "com net org io dev ai cloud co biz xyz tech online eu uk ie de fr nl es it "
    "us ca au nz lan home homelab lab box private local internal intranet corp localdomain arpa"
).split()
HOST_LABEL = r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?"
# Starts after a non-name character, or after the dot of `*.name` or of a leading
# `.name` (a NO_PROXY or cookie domain).
BARE_HOST = re.compile(
    rf"(?:(?<=\*\.)|(?<=^\.)|(?<=[\s\"'`=,(\[]\.)|(?<![\w.-]))((?:{HOST_LABEL}\.)+(?:{'|'.join(HOST_TLDS)}))(?![\w(-]|\.[a-z0-9])", re.IGNORECASE
)
ESCAPE = re.compile(r"\\[nrt]")
# A URL's host: after the scheme and any user:password@, up to the port or path.
URL_HOST = re.compile(r"\b[a-z][a-z0-9+.-]*://(?:[^\s/@'\"`<>]*@)?([^\s:/?#'\"`<>()\[\],;|\\]+)", re.IGNORECASE)

# `nats stream add`, `nats kv edit`, `nats object add`, and the CLI's aliases, with
# any flags between.
NATS_DEFINE = re.compile(
    r"\bnats\b.*?\b(?:stream|str|kv|object|obj)\b[\s\"',]+(?:add|create|new|edit|update)\b"
)
# In Python only an argument list is a command (`["nats", "kv", "add", ...]`), so a
# docstring or comment that names one is not.
NATS_DEFINE_PY = re.compile(
    r"""["']nats["']\s*,\s*(?:["'][^"']*["']\s*,\s*)*?["'](?:stream|str|kv|object|obj)["']\s*,"""
    r"""\s*["'](?:add|create|new|edit|update)["']"""
)
COMMAND_FILES = re.compile(r"(\.(sh|bash|ya?ml|mk|py)$)|(^|/)(Makefile|Dockerfile[^/]*|Containerfile[^/]*)$")
# A YAML block list item that is a scalar (`- nats`, `- "stream"`, `- nats://x:4222`),
# not a mapping (`- name: x`) or a comment.
LIST_ITEM = re.compile(r"^(\s*)- ((?:(?!:\s|:$|\s#)[^#])(?:(?!:\s|:$|\s#).)*?)\s*$")
FENCE = re.compile(r"^\s*(```|~~~)")


class Allowlist:
    """The public hosts the scan accepts, each with its reason, and which were used."""

    def __init__(self, entries: dict[str, int]) -> None:
        self.entries = entries
        self.used: set[str] = set()

    def allows(self, host: str) -> bool:
        for entry in self.entries:
            if host == entry or host.endswith("." + entry):
                self.used.add(entry)
                return True
        return False


def read_allowlist(text: str, where: str = str(ALLOW)) -> tuple[Allowlist, list[str]]:
    """The allowlist's entries (host to line number), and the lines that are not one."""
    entries: dict[str, int] = {}
    problems: list[str] = []
    for n, line in enumerate(text.splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        parts = line.split(None, 1)
        host = parts[0].lower()
        if len(parts) < 2 or not parts[1].strip():
            problems.append(f"allowlist: {where}:{n}: {host} has no reason")
        elif not re.fullmatch(rf"(?:{HOST_LABEL}\.)+{HOST_LABEL}", host):
            problems.append(f"allowlist: {where}:{n}: {host} is not a host name")
        elif host in entries:
            problems.append(f"allowlist: {where}:{n}: {host} is listed twice")
        else:
            entries[host] = n
    return Allowlist(entries), problems


def placeholder(host: str) -> bool:
    return any(host == s or host.endswith("." + s) for s in PLACEHOLDER_SUFFIXES)


def addresses(line: str) -> list[str]:
    """The IPv4 addresses on a line that are not documentation, loopback or 0.0.0.0."""
    found = []
    for m in IPV4.finditer(line):
        try:
            ip = ipaddress.IPv4Address(m.group(1))
        except ValueError:
            continue  # an octet over 255: a version or an OID, not an address
        if not any(ip in net for net in ALLOWED_NETWORKS):
            found.append(m.group(1))
    return found


def hosts(line: str, allow: Allowlist) -> list[str]:
    """The host names on a line that are not placeholders, in-cluster or allowed."""
    names = [m.group(1) for m in URL_HOST.finditer(line)] + [m.group(1) for m in BARE_HOST.finditer(line)]
    found: list[str] = []
    for name in names:
        host = name.lower().rstrip(".")
        if host in found or "." not in host or "$" in host or "{" in host:
            continue  # no dot is a Service or localhost; $ and { are a template
        if IPV4.fullmatch(host) or placeholder(host) or allow.allows(host):
            continue
        found.append(host)
    return found


def command_lines(path: str, lines: list[str]) -> list[tuple[int, str]]:
    """The lines of a file that are commands: not comments, and in Markdown only
    those inside a code block. Joined as in logical_lines."""
    if path.endswith(".md"):
        out, fenced = [], False
        for n, line in enumerate(lines, 1):
            if FENCE.match(line):
                fenced = not fenced
            elif fenced:
                out.append((n, line))
        return logical_lines(out)
    if COMMAND_FILES.search(path):
        return logical_lines([(n, line) for n, line in enumerate(lines, 1) if not line.lstrip().startswith("#")])
    return []


def logical_lines(lines: list[tuple[int, str]]) -> list[tuple[int, str]]:
    """A command as one line, numbered by its first: a line ending in a backslash
    joined to the next, and a YAML block list's scalar items (`- nats`, `- stream`,
    `- add`, a Job's args) joined into one."""
    out: list[tuple[int, str]] = []
    cont: tuple[int, str] | None = None  # a line ending in \, waiting for the rest
    items: tuple[int, str, str] | None = None  # a list's first line, indent, items so far
    for n, line in lines:
        if cont is not None:
            n, line = cont[0], cont[1] + " " + line
            cont = None
        if line.rstrip().endswith("\\"):
            cont = (n, line.rstrip()[:-1])
            continue
        m = LIST_ITEM.match(line)
        if items is not None and m and m.group(1) == items[1]:
            items = (items[0], items[1], items[2] + " " + m.group(2))
            continue
        if items is not None:
            out.append((items[0], items[2]))
            items = None
        if m:
            items = (n, m.group(1), m.group(2))
        else:
            out.append((n, line))
    if cont is not None:
        out.append(cont)
    if items is not None:
        out.append((items[0], items[2]))
    return out


def scan(path: str, text: str, allow: Allowlist) -> list[str]:
    """Every failure in one file, as `rule: file:line: what`."""
    problems: list[str] = []
    lines = text.splitlines()
    for n, line in enumerate(lines, 1):
        # A \n or \t escape in a string is a separator, not the start of a name.
        line = ESCAPE.sub(" ", line)
        for a in addresses(line):
            problems.append(
                f"address: {path}:{n}: {a} is outside the documentation ranges and loopback; use 192.0.2.x"
            )
        for h in hosts(line, allow):
            problems.append(
                f"host: {path}:{n}: {h} is not a placeholder; use example.com, or allow a public host "
                f"in {ALLOW} with a reason"
            )
    if path != BOOTSTRAP:
        define = NATS_DEFINE_PY if path.endswith(".py") else NATS_DEFINE
        for n, line in command_lines(path, lines):
            if define.search(line):
                problems.append(
                    f"bootstrap: {path}:{n}: streams and buckets are added or changed only by {BOOTSTRAP}"
                )
    return problems


def files(root: Path) -> list[str]:
    """Every file git tracks, and every new one it does not ignore."""
    out = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=root,
        check=True,
        capture_output=True,
    ).stdout.decode()
    return sorted({p for p in out.split("\0") if p and (root / p).is_file()})


def read_text(path: Path) -> str | None:
    """A file's text, or None for a binary file."""
    data = path.read_bytes()
    if b"\0" in data:
        return None
    try:
        return data.decode("utf-8")
    except UnicodeDecodeError:
        return None


def main(root: Path = ROOT) -> int:
    allow, problems = read_allowlist((root / ALLOW).read_text(encoding="utf-8"))
    scanned = 0
    for p in files(root):
        if Path(p).name in SKIPPED_NAMES:
            continue
        text = read_text(root / p)
        if text is None:
            continue
        scanned += 1
        # The allowlist names every entry, so it is read against a copy: its own
        # lines would otherwise count each entry as used. Its addresses and its
        # bootstrap lines are still checked.
        problems += scan(p, text, Allowlist(dict(allow.entries)) if p == str(ALLOW) else allow)
    for host in sorted(set(allow.entries) - allow.used):
        problems.append(f"allowlist: {ALLOW}:{allow.entries[host]}: {host} is used by no file; remove it")
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"private-scan: {len(problems)} problem(s); the rules are in deploy/README.md", file=sys.stderr)
        return 1
    print(f"private-scan: ok, {scanned} files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
