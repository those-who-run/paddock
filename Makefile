# paddock's checks. `make check` is the Definition of done (AGENTS.md): it runs
# what CI runs, the commit hooks on every file (CI's `checks / checks`), and
# `make test`, `make nats-it` and `make compose-it` (CI's `test`, which calls these
# same targets so the three can't drift). Needs python3 and pre-commit; the guard copy
# check needs the network, deploy-render the network, curl and tar, nats-it docker or
# podman, and compose-it docker with compose v2 and curl.
#
# The walking skeleton (deploy/compose, paddock#39) has its own targets: up, replay,
# sub, export, down and reset. `make parity` runs the parity harness (parity/,
# paddock#40) on its default recordings; it is not part of `check`, since CI runs it
# in a workflow of its own (parity-laps.yml).

PY ?= python3
# The compose command; `COMPOSE='podman compose' make up` for podman.
COMPOSE ?= docker compose
# `-p twr` so COMPOSE_PROJECT_NAME in the environment cannot move the stack to another
# project. `run nats-box` and `run replay` need no profile flag, since naming a service
# enables its profile; only down and reset pass `--profile tools --profile replay`, so
# they also remove nats-box's and replay's containers, and `up` starts neither.
STACK = $(COMPOSE) -p twr -f deploy/compose/compose.yaml
# `make replay`: the recording, a file in TWR_RECORDINGS (paddock's recordings/ unless
# set), ecu's --speed, and the run it publishes under, as source replay-$(RUN).
# `make export`: the source whose laps it prints. SPEED and SRC default to ecu's and
# logbook's own defaults.
FILE ?=
SPEED ?= 1
RUN ?= compose
SRC ?= live

.PHONY: help
help:
	@echo "check  - everything a PR must pass: the commit hooks on every file, then make test (CI runs both)"
	@echo "test   - paddock's own checks: links, labels, the guard copy, and their tests (CI's test job)"
	@echo "links  - every internal link and anchor in the Markdown resolves (offline)"
	@echo "labels - docs/labels.md and .github/labels.yml list the same labels"
	@echo "guard  - .claude/ holds those-who-run/.github's guard and settings at the pinned commit"
	@echo "nats   - deploy/nats/: stream subjects match contracts' subjects.yaml, no overlaps, every limit set (offline)"
	@echo "check-deploy - the portable-base rules (deploy/README.md): deploy-render, then the private-detail scan of every file"
	@echo "deploy-render - kustomize build --enable-helm of deploy/'s kustomizations, kubeconform strict, and their checks"
	@echo "nats-it - deploy/nats/bootstrap.sh twice against a real nats-server -js, and what the streams capture"
	@echo "compose-it - the walking skeleton from nothing: up, NATS, the bootstrap, logbook's health, export, reset"
	@echo "parity - the parity harness on its three default recordings, v1's laps against v2's (CI's parity-laps job)"
	@echo
	@echo "The walking skeleton on this machine (deploy/compose/README.md):"
	@echo "up     - start nats, run the stream and KV bootstrap, start logbook and wait for its /healthz"
	@echo "replay - FILE=<name in recordings/> [SPEED=1|N|max] [RUN=compose]: ecu replay onto the bus, as source replay-RUN"
	@echo "sub    - print everything on twr.> through nats-box (Ctrl-C to stop)"
	@echo "export - [SRC=live]: stop logbook, print the source's stored laps (logbook export --laps), start it again"
	@echo "down   - stop and remove the containers; the volumes and logbook's data stay"
	@echo "reset  - down, and remove the volumes: NATS's streams and logbook's data"

.PHONY: check
check:
	pre-commit run --all-files
	$(MAKE) test
	$(MAKE) nats-it
	$(MAKE) compose-it

.PHONY: test
test: links labels guard nats check-deploy
	$(PY) -m unittest discover -s tests

.PHONY: links
links:
	$(PY) scripts/docs_links.py

.PHONY: labels
labels:
	$(PY) scripts/labels_check.py

.PHONY: guard
guard:
	$(PY) scripts/guard_copy.py

.PHONY: nats
nats:
	$(PY) scripts/nats_check.py

# The portable-base rules (deploy/README.md, paddock#50): the rendered manifests'
# checks, then a scan of every file for private addresses, host names and stream
# definitions outside the bootstrap.
.PHONY: check-deploy
check-deploy: deploy-render
	$(PY) scripts/private_scan.py

.PHONY: deploy-render
deploy-render:
	scripts/deploy_render.sh

.PHONY: nats-it
nats-it:
	scripts/nats_it.sh

.PHONY: compose-it
compose-it:
	scripts/compose_it.sh

# `parity laps` on R4's three recordings (parity/README.md), through v1's image and
# the compose stack's pinned images: exit 0 when every recording's laps match. Needs
# Go, docker with compose v2 (CONTAINER and COMPOSE name others), and 127.0.0.1:4222,
# :8222 and :8080 free, so a `make up` stack must be down.
.PHONY: parity
parity:
	$(MAKE) -C parity laps

# The walking skeleton. Each target prints the commands it runs.

.PHONY: up
up:
	$(STACK) up -d
	deploy/compose/wait-healthy.sh

# ecu replay, once, in the `replay` service (profile `replay`; naming it enables it).
# Without --no-deps, so NATS and the bootstrap are started first if they are not
# running; logbook stores what is replayed once it runs (`make up`). ecu's exit code
# is the target's: 0 once everything is published.
.PHONY: replay
replay:
	@test -n "$(FILE)" || { echo "make replay FILE=<a file in recordings/> [SPEED=1|N|max] [RUN=compose]" >&2; exit 2; }
	$(STACK) run --rm -T replay replay --speed $(SPEED) --run $(RUN) /recordings/$(FILE)

.PHONY: sub
sub:
	$(STACK) run --rm nats-box nats --no-context sub 'twr.>'

# `logbook export` takes the store's lock and refuses while logbook runs, so logbook
# is stopped for it and started again whether or not the export worked. --no-deps on
# both, so neither reruns the bootstrap.
.PHONY: export
export:
	$(STACK) stop logbook
	$(STACK) run --rm --no-deps -T logbook export --data /data --laps --src $(SRC); \
		status=$$?; $(STACK) up -d --no-deps logbook; exit $$status

.PHONY: down
down:
	$(STACK) --profile tools --profile replay down --remove-orphans

.PHONY: reset
reset:
	$(STACK) --profile tools --profile replay down --remove-orphans --volumes
