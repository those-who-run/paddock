#!/bin/sh
# The bootstrap against a real, empty nats-server -js (paddock#36, AC1, AC2, AC4 and
# R4's refusal). scripts/nats_it.sh starts the server and runs this in the same
# pinned nats-box the bootstrap uses, with deploy/nats mounted read-only at the
# directory given (default: this script's parent), NATS_URL set, and WORK a
# writable directory (a tmpfs) for the changed copies of the definitions.
#
# The expected settings are written out here from the issue, not read from the
# definition files, so a change to a file that drifts from the spec fails.

set -eu

dir=${1:-$(dirname "$0")/..}
WORK=${WORK:?WORK must name a writable directory}
failed=0

n() {
	nats --no-context "$@"
}

fail() {
	echo "FAIL: $*" >&2
	failed=1
}

# Wait for the server and JetStream, up to 30 s.
i=0
until n stream ls --names >/dev/null 2>&1; do
	i=$((i + 1))
	if [ "$i" -ge 30 ]; then
		echo "FAIL: no JetStream at $NATS_URL after 30 s" >&2
		exit 1
	fi
	sleep 1
done

# The named field of a stream's config, as compact JSON.
field() {
	n stream info "$1" --json | jq -c ".config.$2"
}

expect() {
	got=$(field "$1" "$2")
	[ "$got" = "$3" ] || fail "$1 $2 is $got, want $3"
}

# A stream's message count, waited for: a core publish is stored a moment after
# nats pub returns.
count() {
	n stream info "$1" --json | jq '.state.messages'
}

expect_count() {
	i=0
	while [ "$(count "$1")" -ne "$2" ] && [ "$i" -lt 20 ]; do
		i=$((i + 1))
		sleep 0.1
	done
	got=$(count "$1")
	[ "$got" -eq "$2" ] || fail "$1 holds $got messages, want $2"
}

echo "== AC1: the first run creates both streams and the bucket"
out=$("$dir/bootstrap.sh" "$dir")
echo "$out"
for want in "stream TELEMETRY created" "stream EVENTS created" "bucket live created"; do
	printf '%s\n' "$out" | grep -qxF "bootstrap: $want" || fail "first run did not say: $want"
done

n stream info TELEMETRY
expect TELEMETRY subjects '["twr.v1.*.frame.gt7"]'
expect TELEMETRY storage '"file"'
expect TELEMETRY retention '"limits"'
expect TELEMETRY max_age 7200000000000
expect TELEMETRY discard '"old"'
expect TELEMETRY num_replicas 1
expect TELEMETRY max_bytes 2147483648
expect TELEMETRY duplicate_window 120000000000

n stream info EVENTS
expect EVENTS subjects '["twr.v1.*.evt.>"]'
expect EVENTS storage '"file"'
expect EVENTS retention '"limits"'
expect EVENTS max_age 2592000000000000
expect EVENTS duplicate_window 120000000000
expect EVENTS discard '"old"'
expect EVENTS num_replicas 1
expect EVENTS max_bytes 1073741824

n kv info live
expect KV_live max_msgs_per_subject 1
expect KV_live storage '"file"'
expect KV_live num_replicas 1
expect KV_live max_msg_size 65536
expect KV_live max_bytes 67108864

echo "== AC2: a second run changes nothing and exits 0"
snapshot() {
	for s in TELEMETRY EVENTS KV_live; do
		n stream info "$s" --json | jq -c '{name: .config.name, config: .config, created: .created}'
	done
}
before=$(snapshot)
out=$("$dir/bootstrap.sh" "$dir")
echo "$out"
printf '%s\n' "$out" | grep -v ' unchanged$' && fail "second run changed something"
[ "$(printf '%s\n' "$out" | wc -l)" -eq 3 ] || fail "second run did not report three definitions"
[ "$(snapshot)" = "$before" ] || fail "the configs differ after the second run"

echo "== AC4: core publishes are captured by the right stream"
n pub twr.v1.parity.frame.gt7 '{"seq":1}'
n pub twr.v1.parity.frame.gt7.lo '{"seq":1}'
n pub twr.v1.parity.evt.x.lap.completed '{"lap":1}'
expect_count TELEMETRY 1
expect_count EVENTS 1
n stream get TELEMETRY --last-for twr.v1.parity.frame.gt7 >/dev/null ||
	fail "TELEMETRY does not hold twr.v1.parity.frame.gt7"
n stream get EVENTS --last-for twr.v1.parity.evt.x.lap.completed >/dev/null ||
	fail "EVENTS does not hold twr.v1.parity.evt.x.lap.completed"
for s in TELEMETRY EVENTS KV_live; do
	n stream get "$s" --last-for twr.v1.parity.frame.gt7.lo >/dev/null 2>&1 &&
		fail "$s holds twr.v1.parity.frame.gt7.lo"
done

echo "== The duplicate window: one event published twice with one Nats-Msg-Id is stored once"
n pub -H 'Nats-Msg-Id:parity:x:lap.completed:2' twr.v1.parity.evt.x.lap.completed '{"lap":2}'
n pub -H 'Nats-Msg-Id:parity:x:lap.completed:2' twr.v1.parity.evt.x.lap.completed '{"lap":2}'
expect_count EVENTS 2

echo "== R4: a definition the server refuses exits non-zero with the server's message"
if err=$("$dir/bootstrap.sh" "$dir/test/refused" 2>&1); then
	fail "the overlapping stream was accepted: $err"
else
	echo "$err"
	printf '%s\n' "$err" | grep -q 'subjects overlap with an existing stream' ||
		fail "the refusal does not carry the server's message"
fi
n stream ls --names | grep -qxF OVERLAP && fail "OVERLAP was created"

echo "== R4: a changed limit in each definition is updated in place, then unchanged"
# A copy of the definitions with one limit changed in each file, as a review
# would change them; the stage after it puts the real ones back.
changed=$WORK/changed
mkdir -p "$changed/streams" "$changed/kv"
jq '.max_bytes = 1073741824' "$dir/streams/telemetry.json" >"$changed/streams/telemetry.json"
jq '.duplicate_window = 60000000000' "$dir/streams/events.json" >"$changed/streams/events.json"
jq '.max_bucket_size = 33554432 | .max_value_size = 32768' "$dir/kv/live.json" >"$changed/kv/live.json"
if out=$("$dir/bootstrap.sh" "$changed"); then
	echo "$out"
	for want in "stream TELEMETRY differs in: max_bytes" "stream TELEMETRY updated" \
		"stream EVENTS differs in: duplicate_window" "stream EVENTS updated" \
		"bucket live differs in: max_msg_size max_bytes" "bucket live updated"; do
		printf '%s\n' "$out" | grep -qxF "bootstrap: $want" || fail "the changed run did not say: $want"
	done
else
	fail "the changed run exited non-zero: $out"
fi
expect TELEMETRY max_bytes 1073741824
expect EVENTS duplicate_window 60000000000
expect KV_live max_bytes 33554432
expect KV_live max_msg_size 32768
expect_count TELEMETRY 1
expect_count EVENTS 2
out=$("$dir/bootstrap.sh" "$changed")
echo "$out"
printf '%s\n' "$out" | grep -v ' unchanged$' && fail "the run after the change changed something"

echo "== R4: the real definitions put the old limits back"
out=$("$dir/bootstrap.sh" "$dir")
echo "$out"
[ "$(printf '%s\n' "$out" | grep -c ' updated$')" -eq 3 ] || fail "the real definitions were not applied again"
expect TELEMETRY max_bytes 2147483648
expect EVENTS duplicate_window 120000000000
expect KV_live max_bytes 67108864
expect KV_live max_msg_size 65536

echo "== R4: a bucket's storage cannot change in place, and the bucket stays as it was"
memory=$WORK/memory
mkdir -p "$memory/kv"
jq '.storage = "memory"' "$dir/kv/live.json" >"$memory/kv/live.json"
if err=$("$dir/bootstrap.sh" "$memory" 2>&1); then
	fail "a change of the bucket's storage was accepted: $err"
else
	echo "$err"
	printf '%s\n' "$err" | grep -qxF "bootstrap: bucket live: storage cannot be changed in place" ||
		fail "the storage refusal does not say why"
fi
expect KV_live storage '"file"'

echo "== R6: a user and password in NATS_URL are never printed"
# The password holds an @ of its own, so a redaction that stops at the first @
# leaks its tail.
user=twr-throwaway-user
password=twr-throwaway@pass-word
with_creds=$(printf '%s' "$NATS_URL" | sed "s#//#//$user:$password@#")
# Against the server, which has no users and so accepts any.
out=$(NATS_URL=$with_creds "$dir/bootstrap.sh" "$dir" 2>&1) || fail "the run with credentials in NATS_URL failed: $out"
echo "$out"
no_secret() {
	for secret in "$user" twr-throwaway@ pass-word; do
		printf '%s\n' "$1" | grep -qF "$secret" && fail "the output holds $secret: $1"
	done
	return 0
}
no_secret "$out"
# Against a port nothing listens on, where the bootstrap names the server it could
# not reach: with a scheme, without one, and in a list.
for url in "nats://$user:$password@127.0.0.1:1" "$user:$password@127.0.0.1:1" \
	"nats://127.0.0.1:2,nats://$user:$password@127.0.0.1:1"; do
	if out=$(NATS_URL=$url "$dir/bootstrap.sh" "$dir" 2>&1); then
		fail "the run against a closed port succeeded"
	fi
	echo "$out"
	printf '%s\n' "$out" | grep -q "^bootstrap: cannot list streams on .*127\.0\.0\.1:1$" ||
		fail "the unreachable server is not named"
	no_secret "$out"
done

if [ "$failed" -ne 0 ]; then
	echo "nats integration: FAILED" >&2
	exit 1
fi
echo "nats integration: passed"
