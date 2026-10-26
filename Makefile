# paddock's checks. `make check` is the Definition of done (AGENTS.md): it runs
# what CI runs, the commit hooks on every file (CI's `checks / checks`), and
# `make test` and `make nats-it` (CI's `test`, which calls these same targets so the
# two can't drift). Needs python3 and pre-commit; the guard copy check needs the
# network, and nats-it docker or podman.

PY ?= python3

.PHONY: help
help:
	@echo "check  - everything a PR must pass: the commit hooks on every file, then make test (CI runs both)"
	@echo "test   - paddock's own checks: links, labels, the guard copy, and their tests (CI's test job)"
	@echo "links  - every internal link and anchor in the Markdown resolves (offline)"
	@echo "labels - docs/labels.md and .github/labels.yml list the same labels"
	@echo "guard  - .claude/ holds those-who-run/.github's guard and settings at the pinned commit"
	@echo "nats   - deploy/nats/: stream subjects match contracts' subjects.yaml, no overlaps, every limit set (offline)"
	@echo "nats-it - deploy/nats/bootstrap.sh twice against a real nats-server -js, and what the streams capture"

.PHONY: check
check:
	pre-commit run --all-files
	$(MAKE) test
	$(MAKE) nats-it

.PHONY: test
test: links labels guard nats
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

.PHONY: nats-it
nats-it:
	scripts/nats_it.sh
