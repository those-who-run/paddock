# The parity harness's own checks, the steps of those-who-run/.github's go.yml in
# its order, so a laptop runs what the Go job runs once CI calls it with
# `working-directory: parity` (paddock#40 R6). Needs Go (the version in go.mod's
# toolchain line); the first run of the tools needs the network.
#
# `make laps` runs the harness itself: docker (CONTAINER names another runtime) and
# compose (COMPOSE), as paddock's own Makefile takes them.

# golangci-lint and govulncheck at the versions tools/go.mod pins and tools/go.sum
# checksums, as go.yml runs them: by package path, outside any go.work.
GO_TOOL := GOWORK=off go tool -modfile=tools/go.mod
GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck
# The lowest total statement coverage the tests may reach, in per cent: go.yml's
# default, which the CI job is to pass.
COVERAGE_FLOOR := 90
GO_FILES = $(shell find . -name '*.go' -not -path '*/.*' -not -path '*/testdata/*')
BUILD := build
# The recordings to compare; empty is R4's three.
RECORDINGS ?=
# What `make events` compares: runs, or --all; empty is R4's three.
RUNS ?=

.PHONY: help
help:
	@echo "check - the Go checks in go.yml's order: fmt-check, tidy, vet, lint, test, build, vuln"
	@echo "test  - the tests, with the race detector, in shuffled order, no skips, and the coverage floor"
	@echo "laps  - the harness on RECORDINGS (default: R4's three), through v1's image and the compose stack"
	@echo "events - events and callouts on RUNS (default: R4's three; --all for every recording and the restart pair)"
	@echo "fmt   - format the Go code with gofmt"

.PHONY: check
check: fmt-check tidy vet lint test build vuln

.PHONY: fmt
fmt:
	gofmt -w $(GO_FILES)

.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l $(GO_FILES)) || exit 1; \
	if [ -n "$$unformatted" ]; then echo "not formatted (make fmt):" $$unformatted >&2; exit 1; fi

.PHONY: tidy
tidy:
	go mod tidy -diff
	cd tools && go mod tidy -diff

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	$(GO_TOOL) $(GOLANGCI_LINT) run ./...

.PHONY: test
test:
	@mkdir -p $(BUILD)
	CGO_ENABLED=1 go test -race -shuffle=on -covermode=atomic -coverprofile=$(BUILD)/cover.out -v ./... > $(BUILD)/test.log 2>&1; \
	  status=$$?; grep -v -E '^(=== |\s+--- PASS|--- PASS)' $(BUILD)/test.log; exit $$status
	@if grep -E '^\s*--- SKIP' $(BUILD)/test.log; then echo "a skipped test fails: fix it or remove it" >&2; exit 1; fi
	@total=$$(go tool cover -func=$(BUILD)/cover.out | awk '/^total:/ {sub("%", "", $$3); print $$3}'); \
	  echo "coverage $$total%, floor $(COVERAGE_FLOOR)%"; \
	  awk -v t="$$total" -v f="$(COVERAGE_FLOOR)" 'BEGIN { exit !(t >= f) }' || { echo "coverage under the floor" >&2; exit 1; }

.PHONY: build
build:
	go build -o /dev/null ./...

.PHONY: vuln
vuln:
	$(GO_TOOL) $(GOVULNCHECK) ./...

.PHONY: laps
laps:
	go run . laps $(RECORDINGS)

.PHONY: events
events:
	go run . events $(RUNS)
