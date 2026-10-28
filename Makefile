# paddock's checks. `make check` is the Definition of done (AGENTS.md): it runs
# what CI runs, the commit hooks on every file (CI's `checks / checks`), and
# `make test`, `make nats-it` and `make compose-it` (CI's `test`, which calls these
# same targets so the three can't drift). Needs python3 and pre-commit; the guard copy
# check needs the network, deploy-render the network, curl and tar, nats-it docker or
# podman, and compose-it docker with compose v2 and curl.
#
# The walking skeleton (deploy/compose, paddock#39) has its own targets: up, sub,
# export, down and reset. `replay` comes with ecu's replay (paddock#35).

PY ?= python3
# The compose command; `COMPOSE='podman compose' make up` for podman.
COMPOSE ?= docker compose
# `-p twr` so COMPOSE_PROJECT_NAME in the environment cannot move the stack to another
# project. `run nats-box` needs no profile flag, since naming a service enables its
# profile; only down and reset pass `--profile tools`, so they also remove nats-box's
# containers, and `up` does not start it.
STACK = $(COMPOSE) -p twr -f deploy/compose/compose.yaml

.PHONY: help
help:
	@echo "check  - everything a PR must pass: the commit hooks on every file, then make test (CI runs both)"
	@echo "test   - paddock's own checks: links, labels, the guard copy, and their tests (CI's test job)"
	@echo "links  - every internal link and anchor in the Markdown resolves (offline)"
	@echo "labels - docs/labels.md and .github/labels.yml list the same labels"
	@echo "guard  - .claude/ holds those-who-run/.github's guard and settings at the pinned commit"
	@echo "nats   - deploy/nats/: stream subjects match contracts' subjects.yaml, no overlaps, every limit set (offline)"
	@echo "deploy-render - kustomize build --enable-helm of deploy/'s kustomizations, kubeconform strict, and their checks"
	@echo "nats-it - deploy/nats/bootstrap.sh twice against a real nats-server -js, and what the streams capture"
	@echo "compose-it - the walking skeleton from nothing: up, NATS, the bootstrap, logbook's health, export, reset"
	@echo
	@echo "The walking skeleton on this machine (deploy/compose/README.md):"
	@echo "up     - start nats, run the stream and KV bootstrap, start logbook and wait for its /healthz"
	@echo "sub    - print everything on twr.> through nats-box (Ctrl-C to stop)"
	@echo "export - stop logbook, print its stored laps (logbook export --laps), start it again"
	@echo "down   - stop and remove the containers; the volumes and logbook's data stay"
	@echo "reset  - down, and remove the volumes: NATS's streams and logbook's data"

.PHONY: check
check:
	pre-commit run --all-files
	$(MAKE) test
	$(MAKE) nats-it
	$(MAKE) compose-it

.PHONY: test
test: links labels guard nats deploy-render
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

.PHONY: deploy-render
deploy-render:
	scripts/deploy_render.sh

.PHONY: nats-it
nats-it:
	scripts/nats_it.sh

.PHONY: compose-it
compose-it:
	scripts/compose_it.sh

# The walking skeleton. Each target prints the commands it runs.

.PHONY: up
up:
	$(STACK) up -d
	deploy/compose/wait-healthy.sh

.PHONY: sub
sub:
	$(STACK) run --rm nats-box nats --no-context sub 'twr.>'

# `logbook export` takes the store's lock and refuses while logbook runs, so logbook
# is stopped for it and started again whether or not the export worked. --no-deps on
# both, so neither reruns the bootstrap.
.PHONY: export
export:
	$(STACK) stop logbook
	$(STACK) run --rm --no-deps -T logbook export --data /data --laps; \
		status=$$?; $(STACK) up -d --no-deps logbook; exit $$status

.PHONY: down
down:
	$(STACK) --profile tools down --remove-orphans

.PHONY: reset
reset:
	$(STACK) --profile tools down --remove-orphans --volumes
