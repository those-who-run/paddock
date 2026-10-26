#!/bin/sh
# Create or update the JetStream streams and KV buckets from the definition files
# (paddock#36). Runs in natsio/nats-box, which has the nats CLI and jq; it is POSIX
# sh because that image has no bash. Compose (paddock#39) and the cluster's Job
# (paddock#43) run it with the same files.
#
#   bootstrap.sh [DIR]
#
# DIR holds streams/*.json (the nats CLI's --config format) and kv/*.json (one
# bucket each, the fields named after nats kv add's flags); it defaults to this
# script's directory. The server and credentials come from the environment only:
# NATS_URL (required), and NATS_CREDS or NATS_USER/NATS_PASSWORD when set, which
# the nats CLI reads itself. --no-context keeps a nats context on the machine from
# taking their place.
#
# Each stream or bucket is created when it is missing, updated when a field in its
# file differs from the server's, and left alone otherwise, so a second run changes
# nothing. Only the fields a file names are compared; "$comment" is documentation.
# Nothing is ever deleted or purged: a change the server cannot make in place
# (storage, retention, overlapping subjects) fails with the server's message and a
# non-zero exit, and the stream is left as it was. It writes no files, so it runs
# on a read-only root file system.

set -eu

dir=${1:-$(dirname "$0")}

if [ -z "${NATS_URL:-}" ]; then
	echo "bootstrap: NATS_URL is not set (for example nats://localhost:4222)" >&2
	exit 2
fi
if [ -n "${NATS_CREDS:-}" ] && [ ! -r "$NATS_CREDS" ]; then
	echo "bootstrap: NATS_CREDS names $NATS_CREDS, which cannot be read" >&2
	exit 2
fi

n() {
	nats --no-context "$@"
}

# NATS_URL may carry a user and password (nats://user:pass@host:4222, user:pass@host,
# or a list of such URLs); whatever this script prints shows it without them, as a
# Job's log is read by more people than hold the credentials. Each URL loses
# everything up to its last @, since a password may hold an @ of its own.
server=$(printf '%s' "$NATS_URL" | sed -E 's#(^|[ ,]|//)[^/, ]*@#\1#g')

# The config of the stream named $1, as compact JSON, or a clear error.
config() {
	if ! info=$(n stream info "$1" --json); then
		echo "bootstrap: cannot read stream $1 on $server" >&2
		exit 1
	fi
	printf '%s' "$info" | jq -c .config
}

# Also the check that the server answers and has JetStream: the CLI's own error
# says which it is.
if ! existing=$(n stream ls --names); then
	echo "bootstrap: cannot list streams on $server" >&2
	exit 1
fi

exists() {
	printf '%s\n' "$existing" | grep -qxF "$1"
}

# The names of the fields in $want (a JSON object) whose values differ from those
# in $have, on one line. Arrays are compared sorted, as the server may reorder
# subjects.
differs() {
	jq -rn --argjson want "$1" --argjson have "$2" '
		def norm: if type == "array" then sort else . end;
		[$want | to_entries[] | select((.value | norm) != ($have[.key] | norm)) | .key]
		| join(" ")'
}

streams=0
for file in "$dir"/streams/*.json; do
	[ -e "$file" ] || continue
	streams=$((streams + 1))
	want=$(jq -c 'with_entries(select(.key | startswith("$") | not))' "$file")
	name=$(printf '%s' "$want" | jq -r '.name // empty')
	if [ -z "$name" ]; then
		echo "bootstrap: $file has no name" >&2
		exit 1
	fi
	if ! exists "$name"; then
		n stream add --config "$file" >/dev/null
		echo "bootstrap: stream $name created"
		continue
	fi
	have=$(config "$name")
	changed=$(differs "$want" "$have")
	if [ -z "$changed" ]; then
		echo "bootstrap: stream $name unchanged"
		continue
	fi
	echo "bootstrap: stream $name differs in: $changed"
	n stream edit "$name" --config "$file" --force >/dev/null
	echo "bootstrap: stream $name updated"
done

buckets=0
for file in "$dir"/kv/*.json; do
	[ -e "$file" ] || continue
	buckets=$((buckets + 1))
	# The bucket's fields as the backing stream KV_<bucket> names them, so they can
	# be compared with nats stream info; a field with no mapping fails here rather
	# than being ignored.
	if ! want=$(jq -ce '
		with_entries(select(.key | startswith("$") | not))
		| . as $f
		| {bucket: "name", description: "description", history: "max_msgs_per_subject",
		   storage: "storage", replicas: "num_replicas", max_value_size: "max_msg_size",
		   max_bucket_size: "max_bytes"} as $map
		| if ($f | keys - ($map | keys)) != [] then
			error("unknown fields: \($f | keys - ($map | keys) | join(", "))")
		  elif ($f.bucket // "") == "" then error("no bucket")
		  else $f | with_entries(.key = $map[.key]) | .name = "KV_" + .name end' "$file"); then
		echo "bootstrap: $file cannot be read as a bucket" >&2
		exit 1
	fi
	bucket=$(jq -r .bucket "$file")
	set -- --history "$(jq -r '.history // 1' "$file")" \
		--replicas "$(jq -r '.replicas // 1' "$file")"
	for field in description max_value_size max_bucket_size; do
		value=$(jq -r --arg f "$field" '.[$f] // empty' "$file")
		[ -z "$value" ] || set -- "$@" "--$(printf '%s' "$field" | tr _ -)=$value"
	done
	if ! exists "KV_$bucket"; then
		n kv add "$bucket" --storage "$(jq -r '.storage // "file"' "$file")" "$@" >/dev/null
		echo "bootstrap: bucket $bucket created"
		continue
	fi
	have=$(config "KV_$bucket")
	changed=$(differs "$want" "$have")
	if [ -z "$changed" ]; then
		echo "bootstrap: bucket $bucket unchanged"
		continue
	fi
	echo "bootstrap: bucket $bucket differs in: $changed"
	case " $changed " in
	*" storage "*)
		echo "bootstrap: bucket $bucket: storage cannot be changed in place" >&2
		exit 1
		;;
	esac
	n kv edit "$bucket" "$@" >/dev/null
	echo "bootstrap: bucket $bucket updated"
done

if [ "$streams" -eq 0 ] && [ "$buckets" -eq 0 ]; then
	echo "bootstrap: no definitions in $dir/streams or $dir/kv" >&2
	exit 1
fi
