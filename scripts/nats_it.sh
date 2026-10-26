#!/bin/sh
# Run deploy/nats/bootstrap.sh against a real, empty nats-server -js and check the
# result (paddock#36): `make nats-it` and CI's `test` job run it. It starts the
# server in a container, then runs deploy/nats/test/integration.sh in the pinned
# nats-box the bootstrap uses, as user 1000 on a read-only root file system, as the
# cluster's Job does (paddock#43), and removes the server whatever happens.
#
# Needs docker or podman: CONTAINER names one, or docker is used if it is there,
# else podman. The server listens on 127.0.0.1 only, on NATS_IT_PORT (default
# 14222, away from a NATS on 4222).

set -eu

if [ -z "${CONTAINER:-}" ]; then
	if command -v docker >/dev/null 2>&1; then
		CONTAINER=docker
	elif command -v podman >/dev/null 2>&1; then
		CONTAINER=podman
	else
		echo "nats-it: needs docker or podman" >&2
		exit 1
	fi
fi
PORT=${NATS_IT_PORT:-14222}

# Pinned by the multi-arch index digest; the tag is for people. nats-box is the image
# the bootstrap runs in (#36 R4); nats-server is the server it is tested against.
NATS_BOX=docker.io/natsio/nats-box:0.20.0@sha256:d2f04223f40e016f77922a8115d34b0cd5fad2ec10b19b39316ae92d07f3d295
NATS_SERVER=docker.io/library/nats:2.15.1-alpine@sha256:7ae34c25dfd6fe2e01c6483eabef20d796690fdcac6c4d94ca780a6cf9877fde

root=$(cd "$(dirname "$0")/.." && pwd)
name=twr-nats-it-$$

cleanup() {
	"$CONTAINER" rm -f "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

"$CONTAINER" run -d --rm --name "$name" -p "127.0.0.1:$PORT:4222" "$NATS_SERVER" -js >/dev/null

# :z relabels the mount where SELinux is on (podman on Fedora) and is ignored
# elsewhere.
status=0
"$CONTAINER" run --rm --network host --read-only --user 1000 \
	-v "$root/deploy/nats:/nats:ro,z" \
	-e "NATS_URL=nats://127.0.0.1:$PORT" \
	"$NATS_BOX" sh /nats/test/integration.sh /nats || status=$?
if [ "$status" -ne 0 ]; then
	echo "== the server's log" >&2
	"$CONTAINER" logs "$name" >&2 || true
fi
exit "$status"
