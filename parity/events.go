package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
)

// defaultNATS is NATS as the compose file publishes it.
const defaultNATS = "nats://127.0.0.1:4222"

// restartPair is v1's two-recording restart case: the engineer stopped before the
// race went on, then started again part way through it, on one store (R5).
var restartPair = []string{
	"real-nuc-suzuka-before-restart-2026-10-18.jsonl.gz",
	"real-nuc-suzuka-joined-mid-race-2026-10-18.jsonl.gz",
}

// Run is one or more recordings replayed in order onto one store and one bus: a
// recording on its own, or the restart pair.
type Run struct {
	Recordings []Recording
}

// Name is the run's recordings' names joined by +, as the command takes them.
func (r Run) Name() string {
	names := make([]string, len(r.Recordings))
	for i, rec := range r.Recordings {
		names[i] = rec.Name
	}
	return strings.Join(names, "+")
}

// Stem is the run's name without the recordings' extensions.
func (r Run) Stem() string {
	stems := make([]string, len(r.Recordings))
	for i, rec := range r.Recordings {
		stems[i] = rec.Stem()
	}
	return strings.Join(stems, "+")
}

// EventResult is one run's comparison.
type EventResult struct {
	Run    Run
	V1, V2 []Item
	Diffs  []EventDiff
	// Matched counts the pairs with both sides and no difference, by events and
	// callouts; Other counts v2's items of types the v1 side doesn't give.
	Events, Callouts tally
	Other            map[string]int
}

type tally struct{ V1, V2, Matched int }

// Failing is the number of differences no accepted entry allows.
func (r EventResult) Failing() int {
	n := 0
	for _, d := range r.Diffs {
		if d.Accepted == "" {
			n++
		}
	}
	return n
}

// compareItems pairs both sides' items, lists the differences and marks the
// accepted ones (R2, R4).
func compareItems(run Run, v1, v2 []Item) EventResult {
	v1, v2 = lastPerFrame(v1), lastPerFrame(v2)
	r := EventResult{Run: run, V1: v1, V2: v2}
	pairs, other := matchItems(v1, v2)
	r.Other = other
	for _, p := range pairs {
		t := &r.Events
		if p.first().Type == "callout" {
			t = &r.Callouts
		}
		if p.V1 != nil {
			t.V1++
		}
		if p.V2 != nil {
			t.V2++
		}
		diffs := diffPair(p)
		if len(diffs) == 0 {
			t.Matched++
		}
		r.Diffs = append(r.Diffs, diffs...)
	}
	accept(r.Diffs, runView{v1: v1, v2: v2})
	return r
}

func events(ctx context.Context, args []string, stdout, stderr io.Writer, mk sides) int {
	var o options
	fs, root, code := commonFlags("events", &o, stderr)
	if fs == nil {
		return code
	}
	fs.StringVar(&o.nats, "nats", defaultNATS, "NATS, as the compose file publishes it, for the checker to listen on")
	fs.BoolVar(&o.keep, "keep", false, "keep v2's stack and both sides' normalised items, and say where")
	all := fs.Bool("all", false, "every recording in recordings/SOURCES.yaml, then the restart pair")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: parity events [--v1-image IMG] [--compose FILE] [--healthz URL] [--nats URL] [--keep] [--all] RUN…")
		_, _ = fmt.Fprintln(stderr, "RUN is a recording (a path, or a file name in recordings/), or recordings joined by +,")
		_, _ = fmt.Fprintln(stderr, "replayed in order onto one store; with none, R4's three defaults.")
		fs.PrintDefaults()
	}
	if code, ok := parseFlags(fs, args, &o, stderr); !ok {
		return code
	}
	dir, sums, err := loadSources(root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}
	specs := fs.Args()
	switch {
	case *all && len(specs) > 0:
		_, _ = fmt.Fprintln(stderr, "parity: --all takes no RUN")
		return exitRefused
	case *all:
		specs = allRuns(sums)
	case len(specs) == 0:
		specs = defaultRecordings
	}
	// Every recording is found and its sha256 checked before anything runs.
	runs, err := resolveRuns(specs, dir, sums)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}
	v1, v2 := mk(o)
	return compareEvents(ctx, runs, v1, v2, o, stdout, stderr)
}

// allRuns is every recording SOURCES.yaml lists, by name, then the restart pair
// when both of its recordings are listed (R5).
func allRuns(sums map[string]string) []string {
	var specs []string
	for name := range sums {
		specs = append(specs, name)
	}
	sort.Strings(specs)
	if sums[restartPair[0]] != "" && sums[restartPair[1]] != "" {
		specs = append(specs, strings.Join(restartPair, "+"))
	}
	return specs
}

func resolveRuns(specs []string, dir string, sums map[string]string) ([]Run, error) {
	var runs []Run
	var all []string
	for _, spec := range specs {
		all = append(all, strings.Split(spec, "+")...)
	}
	recs, err := resolveRecordings(all, dir, sums)
	if err != nil {
		return nil, err
	}
	for _, spec := range specs {
		n := len(strings.Split(spec, "+"))
		runs = append(runs, Run{Recordings: recs[:n]})
		recs = recs[n:]
	}
	return runs, nil
}

// compareEvents runs every run through both sides, prints each run's differences
// and then the table (R5).
func compareEvents(ctx context.Context, runs []Run, v1, v2 Side, o options, stdout, stderr io.Writer) int {
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
	keepDir := ""
	if o.keep {
		if keepDir, err = os.MkdirTemp("", "parity-events-"); err != nil {
			_, _ = fmt.Fprintln(stderr, "parity:", err)
			return exitRefused
		}
	}
	var results []EventResult
	for _, run := range runs {
		a, err := v1.Events(ctx, run)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "parity: %s: v1: %v\n", run.Name(), err)
			return exitRefused
		}
		b, err := v2.Events(ctx, run)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "parity: %s: v2: %v\n", run.Name(), err)
			return exitRefused
		}
		r := compareItems(run, a, b)
		results = append(results, r)
		if err := writeEventResult(stdout, r); err != nil {
			_, _ = fmt.Fprintln(stderr, "parity:", err)
			return exitRefused
		}
		if keepDir != "" {
			if err := keepItems(keepDir, run, a, b); err != nil {
				_, _ = fmt.Fprintln(stderr, "parity:", err)
				return exitRefused
			}
		}
	}
	if err := writeEventTable(stdout, results); err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}
	if keepDir != "" {
		_, _ = fmt.Fprintf(stdout, "normalised items kept in %s\n", keepDir)
	}
	for _, r := range results {
		if r.Failing() > 0 {
			return exitDiffer
		}
	}
	return exitMatch
}

// writeEventResult prints a run's differences, each as v1's value beside v2's
// with the frame (and the recording, for a run of two), and what was not compared.
func writeEventResult(w io.Writer, r EventResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", r.Run.Name())
	for _, rec := range r.Run.Recordings {
		fmt.Fprintf(&b, "  %s  sha256 %s\n", rec.Name, rec.SHA256)
	}
	if len(r.Diffs) > 0 {
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintf(tw, "\tpart\tat\tsession\titem\tfield\tv1\tv2\t\n")
		for _, d := range r.Diffs {
			note := ""
			if d.Accepted != "" {
				note = "accepted: " + d.Accepted
			}
			_, _ = fmt.Fprintf(tw, "\t%d\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n", d.Part, d.At, d.Session, d.Label, d.Field, d.V1, d.V2, note)
		}
		_ = tw.Flush() // a strings.Builder doesn't fail
	}
	if len(r.Other) > 0 {
		var types []string
		for t, n := range r.Other {
			types = append(types, fmt.Sprintf("%s %d", t, n))
		}
		sort.Strings(types)
		fmt.Fprintf(&b, "  not compared yet (no v1 side): %s\n", strings.Join(types, ", "))
	}
	if n := r.Failing(); n > 0 {
		fmt.Fprintf(&b, "  differ: %d %s\n", n, plural(n, "difference", "differences"))
	} else {
		b.WriteString("  match\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeEventTable prints the table R5 asks for: a line per run with its events and
// callouts on each side and matched, its differences and those accepted.
func writeEventTable(w io.Writer, results []EventResult) error {
	var b strings.Builder
	b.WriteString("\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "run\tevents v1\tv2\tmatched\tcallouts v1\tv2\tmatched\tdifferences\taccepted\t\n")
	matched := 0
	for _, r := range results {
		failing := r.Failing()
		if failing == 0 {
			matched++
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t\n", r.Run.Stem(),
			r.Events.V1, r.Events.V2, r.Events.Matched, r.Callouts.V1, r.Callouts.V2, r.Callouts.Matched,
			failing, len(r.Diffs)-failing)
	}
	_ = tw.Flush()
	fmt.Fprintf(&b, "\nparity events: %d of %d %s match\n", matched, len(results), plural(len(results), "run", "runs"))
	_, err := io.WriteString(w, b.String())
	return err
}

// keepItems writes both sides' normalised items, one JSON object per line.
func keepItems(dir string, run Run, v1, v2 []Item) error {
	for side, items := range map[string][]Item{"v1": v1, "v2": v2} {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		for _, it := range items {
			if err := enc.Encode(it); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, safeName(run.Stem())+".events."+side+".jsonl"), []byte(b.String()), 0o600); err != nil {
			return err
		}
	}
	return nil
}
