#!/bin/sh
# Bring the walking skeleton up from nothing and check it (paddock#39 R6): `make
# compose-it` and CI's `test` job run it. Through the same make targets a laptop
# uses, it
#
#   1. starts from a clean slate (`make reset`);
#   2. brings the stack up (`make up`): nats healthy, the bootstrap exited 0, logbook
#      answering 200 on /healthz (AC1), and nothing else running: nats-box (profile
#      `tools`) is not started by `up`;
#   3. checks NATS's monitoring /healthz with JetStream, and that the bootstrap made
#      TELEMETRY, EVENTS and the live bucket, through nats-box (profile `tools`);
#   4. runs `make export` against the running stack: logbook is stopped, the export
#      reads its volume, logbook starts and is healthy again;
#   5. tears it down (`make reset`) and checks no container or volume of the project
#      is left (AC4).
#
# The replay of real-race-2laps-2026-10-14 and the check that `make export` prints
# its laps (AC2) are added with ecu's replay (paddock#35).
#
# Needs docker with compose v2 (COMPOSE names another, for example `podman compose`;
# CONTAINER the runtime for the leftovers check), make and curl. It publishes
# 127.0.0.1:4222, :8222 and :8080, so a stack already on those ports must be down.

set -eu

CONTAINER=${CONTAINER:-docker}
COMPOSE=${COMPOSE:-docker compose}
MAKE=${MAKE:-make}
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
export COMPOSE
# The project by name, as the Makefile passes it, so COMPOSE_PROJECT_NAME cannot make
# the leftovers check look at another project and pass on nothing.
project=twr

compose() {
	# shellcheck disable=SC2086 # COMPOSE is a command and its arguments
	$COMPOSE -p "$project" -f deploy/compose/compose.yaml --profile tools "$@"
}

ok=0

finish() {
	if [ "$ok" -ne 1 ]; then
		echo "== compose-it failed; the stack's state and logs" >&2
		compose ps -a >&2 || true
		compose logs --no-color >&2 || true
	fi
	$MAKE --no-print-directory reset >/dev/null 2>&1 || true
}
# finish runs once, at exit; Ctrl-C or a TERM exits non-zero, which runs it.
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
	echo "compose-it: $*" >&2
	exit 1
}

echo "== a clean slate"
$MAKE --no-print-directory reset

echo "== make up (AC1)"
$MAKE --no-print-directory up

echo "== only nats and logbook run; the bootstrap exited 0"
running=$(compose ps --status running --services | sort | tr '\n' ' ')
echo "running: $running"
[ "$running" = "logbook nats " ] || fail "running after make up: $running (want logbook nats)"
bootstrap=$(compose ps -aq nats-bootstrap)
[ -n "$bootstrap" ] || fail "no nats-bootstrap container"
code=$("$CONTAINER" inspect -f '{{.State.Status}} {{.State.ExitCode}}' "$bootstrap")
echo "nats-bootstrap: $code"
[ "$code" = "exited 0" ] || fail "nats-bootstrap is $code (want exited 0)"

echo "== NATS answers with JetStream on 127.0.0.1:8222"
curl -fsS --max-time 5 'http://127.0.0.1:8222/healthz?js-enabled-only=true' || fail "NATS /healthz is not ok"
echo

echo "== the bootstrap made the streams and the bucket"
streams=$(compose run --rm -T nats-box nats --no-context stream ls --names) || fail "nats stream ls failed"
echo "$streams"
for want in TELEMETRY EVENTS KV_live; do
	printf '%s\n' "$streams" | grep -qx "$want" || fail "no $want after the bootstrap"
done

echo "== logbook reports connected"
body=$(curl -fsS --max-time 5 http://127.0.0.1:8080/healthz) || fail "logbook /healthz is not 200"
echo "$body"
printf '%s' "$body" | grep -q '"connected": *true' || fail "logbook is not connected"

echo "== make export against the running stack"
$MAKE --no-print-directory export || fail "make export failed"
"$root/deploy/compose/wait-healthy.sh" || fail "logbook is not healthy after make export"

echo "== make reset leaves nothing behind (AC4)"
$MAKE --no-print-directory reset
left=$("$CONTAINER" ps -aq --filter "label=com.docker.compose.project=$project")
[ -z "$left" ] || fail "containers left after make reset: $left"
left=$("$CONTAINER" volume ls -q --filter "label=com.docker.compose.project=$project")
[ -z "$left" ] || fail "volumes left after make reset: $left"

ok=1
echo "compose-it: ok"
