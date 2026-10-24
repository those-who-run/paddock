# paddock's checks. `make check` is the Definition of done (AGENTS.md): it runs
# what CI runs, the commit hooks on every file (CI's `checks / checks`) and
# `make test` (CI's `test`, which calls this same target so the two can't drift).
# Needs python3 and pre-commit; the guard copy check needs the network.

PY ?= python3

.PHONY: help
help:
	@echo "check  - everything a PR must pass: the commit hooks on every file, then make test (CI runs both)"
	@echo "test   - paddock's own checks: links, labels, the guard copy, and their tests (CI's test job)"
	@echo "links  - every internal link and anchor in the Markdown resolves (offline)"
	@echo "labels - docs/labels.md and .github/labels.yml list the same labels"
	@echo "guard  - .claude/ holds those-who-run/.github's guard and settings at the pinned commit"

.PHONY: check
check:
	pre-commit run --all-files
	$(MAKE) test

.PHONY: test
test: links labels guard
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
