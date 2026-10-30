// Command parity is the walking skeleton's referee (paddock#40, paddock#104): it
// replays each recording through v1 and through v2, normalises both sides' stored
// laps, or the events and callouts each side decided, or what each side spoke
// (paddock#64), to the same lines and compares them.
//
//	parity laps [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--frames] RECORDING…
//	parity events [--v1-image IMG] [--compose FILE] [--healthz URL] [--nats URL] [--keep] [--all] RUN…
//	parity spoken [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--all] RECORDING…
//
// The exit code is 0 when every recording matches, 1 when any differs, and 2 when
// the run is refused or a side fails. README.md has the detail.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	exitMatch   = 0
	exitDiffer  = 1
	exitRefused = 2
)

// Side is one system under test: v1's image, or v2's compose stack.
type Side interface {
	// Images names the images the side runs, for the output (R5, AC4).
	Images(ctx context.Context) ([]string, error)
	// Laps replays rec and returns the laps the side stored, normalised. want is
	// the number of laps v1 stored, so v2 knows when its store is complete.
	Laps(ctx context.Context, rec Recording, want int) ([]Lap, error)
	// Events replays a run's recordings, in order, and returns the events and
	// callouts the side decided, normalised (paddock#104).
	Events(ctx context.Context, run Run) ([]Item, error)
}

// options are `parity laps`' flags.
type options struct {
	v1Image string
	compose string
	healthz string
	nats    string
	keep    bool
	frames  bool
	// everyRecording: the runs cover every recording, so an unused accepted
	// entry fails the run (events.go).
	everyRecording bool
}

// sides makes the two sides from the options; tests swap it for canned ones.
type sides func(options) (v1, v2 Side)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli(ctx, os.Args[1:], os.Stdout, os.Stderr, realSides(os.Stderr))
	stop()
	os.Exit(code)
}

func cli(ctx context.Context, args []string, stdout, stderr io.Writer, mk sides) int {
	switch {
	case len(args) > 0 && args[0] == "laps":
		return laps(ctx, args[1:], stdout, stderr, mk)
	case len(args) > 0 && args[0] == "events":
		return events(ctx, args[1:], stdout, stderr, mk)
	case len(args) > 0 && args[0] == "spoken":
		return spoken(ctx, args[1:], stdout, stderr, mk)
	}
	_, _ = fmt.Fprintln(stderr, "usage: parity laps [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--frames] RECORDING…")
	_, _ = fmt.Fprintln(stderr, "       parity events [--v1-image IMG] [--compose FILE] [--healthz URL] [--nats URL] [--keep] [--all] RUN…")
	_, _ = fmt.Fprintln(stderr, "       parity spoken [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--all] RECORDING…")
	return exitRefused
}

func laps(ctx context.Context, args []string, stdout, stderr io.Writer, mk sides) int {
	var o options
	fs, root, code := commonFlags("laps", &o, stderr)
	if fs == nil {
		return code
	}
	fs.BoolVar(&o.keep, "keep", false, "keep both stores and the normalised laps, and say where")
	fs.BoolVar(&o.frames, "frames", false, "also compare the frames per lap, as notes that don't fail the run")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: parity laps [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--frames] RECORDING…")
		_, _ = fmt.Fprintln(stderr, "RECORDING is a path, or a file name in recordings/; with none, R4's three defaults.")
		fs.PrintDefaults()
	}
	if code, ok := parseFlags(fs, args, &o, stderr); !ok {
		return code
	}

	// Every recording is found and its sha256 checked before anything runs (AC3).
	names := fs.Args()
	if len(names) == 0 {
		names = defaultRecordings
	}
	dir, sums, err := loadSources(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}
	recs, err := resolveRecordings(names, dir, sums)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}

	v1, v2 := mk(o)
	return compareAll(ctx, recs, v1, v2, o, stdout, stderr)
}

// commonFlags makes a command's flag set with the flags both commands take. A nil
// set comes with the exit code: the working directory could not be read.
func commonFlags(name string, o *options, stderr io.Writer) (*flag.FlagSet, string, int) {
	cwd, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return nil, "", exitRefused
	}
	root := findRoot(cwd)
	fs := flag.NewFlagSet("parity "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.v1Image, "v1-image", defaultV1Image, "v1's image, by tag (and digest)")
	defaultCompose := ""
	if root != "" {
		defaultCompose = filepath.Join(root, "deploy", "compose", "compose.yaml")
	}
	fs.StringVar(&o.compose, "compose", defaultCompose, "the compose file whose images are v2 (deploy/compose/compose.yaml)")
	// logbook's /healthz as the compose file publishes it; it follows that port when
	// #39's replay stack changes it.
	fs.StringVar(&o.healthz, "healthz", "http://127.0.0.1:8080/healthz", "logbook's /healthz, as the compose file publishes it")
	return fs, root, exitMatch
}

// parseFlags parses args and checks the compose file. ok false comes with the
// exit code: 0 for --help, 2 for a bad flag or no compose file.
func parseFlags(fs *flag.FlagSet, args []string, o *options, stderr io.Writer) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitMatch, false
		}
		return exitRefused, false
	}
	if o.compose == "" {
		_, _ = fmt.Fprintln(stderr, "parity: no paddock checkout around this directory: pass --compose")
		return exitRefused, false
	}
	if abs, err := filepath.Abs(o.compose); err == nil {
		o.compose = abs
	}
	return exitMatch, true
}

// loadSources reads the checkout's recordings/SOURCES.yaml: the directory and each
// file's sha256. With no checkout, both are empty.
func loadSources(root string) (string, map[string]string, error) {
	if root == "" {
		return "", map[string]string{}, nil
	}
	dir := filepath.Join(root, "recordings")
	b, err := os.ReadFile(filepath.Join(dir, "SOURCES.yaml")) //nolint:gosec // the checkout's own index
	if err != nil {
		return "", nil, err
	}
	sums, err := readSources(b)
	return dir, sums, err
}

// compareAll runs every recording through both sides and prints the tables. The
// images come first, so a log always says what was compared (AC4).
func compareAll(ctx context.Context, recs []Recording, v1, v2 Side, o options, stdout, stderr io.Writer) int {
	v1Images, err := v1.Images(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity: v1:", err)
		return exitRefused
	}
	v2Images, err := v2.Images(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity: v2:", err)
		return exitRefused
	}
	_, _ = fmt.Fprintf(stdout, "v1 image: %s\n", strings.Join(v1Images, ", "))
	_, _ = fmt.Fprintf(stdout, "v2 images: %s\n", strings.Join(v2Images, ", "))
	if o.compose != "" {
		_, _ = fmt.Fprintf(stdout, "v2 compose file: %s\n", o.compose)
	}

	keepDir := ""
	if o.keep {
		if keepDir, err = os.MkdirTemp("", "parity-"); err != nil {
			_, _ = fmt.Fprintln(stderr, "parity:", err)
			return exitRefused
		}
	}
	matched := 0
	for _, rec := range recs {
		a, err := v1.Laps(ctx, rec, 0)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "parity: %s: v1: %v\n", rec.Name, err)
			return exitRefused
		}
		// Nothing to compare against: a wrong image, or a recording with no lap in
		// it, would otherwise match a v2 that stored nothing either.
		if len(a) == 0 {
			_, _ = fmt.Fprintf(stderr, "parity: %s: refused: v1 stored no laps\n", rec.Name)
			return exitRefused
		}
		b, err := v2.Laps(ctx, rec, len(a))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "parity: %s: v2: %v\n", rec.Name, err)
			return exitRefused
		}
		r := Result{Recording: rec, V1: a, V2: b, Diffs: compare(a, b, o.frames)}
		if err := writeResult(stdout, r); err != nil {
			_, _ = fmt.Fprintln(stderr, "parity:", err)
			return exitRefused
		}
		if keepDir != "" {
			if err := keepLaps(keepDir, rec, a, b); err != nil {
				_, _ = fmt.Fprintln(stderr, "parity:", err)
				return exitRefused
			}
		}
		if r.Match() {
			matched++
		}
	}
	_, _ = fmt.Fprintf(stdout, "\nparity laps: %d of %d %s match\n", matched, len(recs), plural(len(recs), "recording", "recordings"))
	if keepDir != "" {
		_, _ = fmt.Fprintf(stdout, "normalised laps kept in %s\n", keepDir)
	}
	if matched != len(recs) {
		return exitDiffer
	}
	return exitMatch
}

// keepLaps writes both sides' normalised laps, one JSON object per line (R7).
func keepLaps(dir string, rec Recording, v1, v2 []Lap) error {
	for side, laps := range map[string][]Lap{"v1": v1, "v2": v2} {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		for _, l := range laps {
			if err := enc.Encode(l); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, rec.Stem()+"."+side+".jsonl"), []byte(b.String()), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// realSides are the two sides on this machine: the container runtime in CONTAINER
// (docker by default) and the compose command in COMPOSE (`docker compose`), as
// paddock's Makefile and scripts take them.
func realSides(log io.Writer) sides {
	return func(o options) (Side, Side) {
		container := os.Getenv("CONTAINER")
		if container == "" {
			container = "docker"
		}
		compose := strings.Fields(os.Getenv("COMPOSE"))
		if len(compose) == 0 {
			compose = []string{"docker", "compose"}
		}
		v1 := &v1Side{run: execRunner, container: container, image: o.v1Image, keep: o.keep, log: log}
		if o.nats == "" {
			o.nats = defaultNATS
		}
		v2 := &v2Side{
			run: execRunner, compose: compose, file: o.compose, keep: o.keep, log: log,
			healthz: o.healthz, nats: o.nats, listen: natsListen,
			client: &http.Client{Timeout: 5 * time.Second},
			wait:   5 * time.Minute, settle: 15 * time.Second, quiet: 2 * time.Second, poll: time.Second,
			said: 5 * time.Second,
		}
		return v1, v2
	}
}
