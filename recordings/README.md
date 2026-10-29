# Recordings

v1 recordings copied unchanged from
[those-who-run/those-who-run](https://github.com/those-who-run/those-who-run) at
tag `v1.0.0`, `tests/fixtures/`. They are the one copy in `paddock`: the compose
stack's replay (#39) and the parity harness ([`parity/`](../parity/README.md),
#40) both read them from here, so CI fetches nothing from other repositories at
run time. They are GPL-3.0, like `paddock` and v1. Each is a real drive cut down
with v1's `those-who-run thin`; its entry in v1's
[`FIXTURES.yaml`](https://github.com/those-who-run/those-who-run/blob/v1.0.0/tests/fixtures/FIXTURES.yaml)
says what was driven and what it pins.

[`SOURCES.yaml`](SOURCES.yaml) lists every file with its v1 path, its sha256, its
frame count and a link to that entry. `tests/test_recordings.py`, which
`make test` runs, fails when a file and its entry disagree, when a file is not
listed or when an entry has no file.

- `real-race-2laps-2026-10-14.jsonl.gz`: practice laps, then a two-lap race.
  The compose stack's replay check (#39 AC2).
- `real-nuc-suzuka-no-stop-2026-10-15.jsonl.gz`: a six-lap race at Suzuka East
  with no stop. The harness's clean race (#40 R4).
- `real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz`: a six-lap race at Suzuka East
  with a stop. The harness's pit stop (#40 R4).
- `real-nuc-sugo-restarts-2026-10-18.jsonl.gz`: three two-lap races at SUGO,
  restarted on lap 2 and then on lap 1. The harness's restarts (#40 R4).

To add one, copy it unchanged from v1's `tests/fixtures/` at `v1.0.0` (in a
v1 checkout, `git show v1.0.0:tests/fixtures/<file> > <paddock>/recordings/<file>`)
and add its entry to `SOURCES.yaml`, with the lines of its v1 entry in the link.
