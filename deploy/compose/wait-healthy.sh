#!/bin/sh
# Wait until logbook's GET /healthz answers 200 on the port compose.yaml publishes
# (paddock#39): `make up` runs it after `docker compose up -d`. logbook's image is
# distroless, with no shell or HTTP client, so compose cannot probe it from inside;
# this asks from the host instead. It answers 503 until it is connected to NATS and
# both its consumers are bound.
#
#   wait-healthy.sh [URL]
#
# URL defaults to http://127.0.0.1:8080/healthz. WAIT_SECONDS (default 60) is how
# long it waits before it gives up and exits 1 with the last answer.

set -eu

url=${1:-http://127.0.0.1:8080/healthz}
limit=${WAIT_SECONDS:-60}

# A deadline by the clock, not a count of tries: a try that waits for its 2 s timeout
# would otherwise stretch the limit, and the "after N s" would be tries.
start=$(date +%s)
while :; do
	if body=$(curl -fsS --max-time 2 "$url" 2>/dev/null); then
		echo "logbook healthy after $(($(date +%s) - start)) s: $body"
		exit 0
	fi
	[ $(($(date +%s) - start)) -lt "$limit" ] || break
	sleep 1
done
echo "logbook not healthy after $limit s at $url" >&2
curl -sS --max-time 2 "$url" >&2 || true
echo >&2
exit 1
