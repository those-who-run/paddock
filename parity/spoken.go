package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// brundleRecordings are the recordings paddock#64's acceptance criteria name, in
// their order: a clean race (AC1), two pit stops (AC2), two stops with the lane
// across the line (AC3), a time trial and a timed race (AC4), a session joined
// part way (AC5), and a replay and a demo that must stay silent (AC6).
var brundleRecordings = []string{
	"real-nuc-suzuka-no-stop-2026-10-15.jsonl.gz",
	"real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz",
	"real-nuc-gilles-villeneuve-pit-stop-2026-10-17.jsonl.gz",
	"real-nuc-sugo-two-stops-2026-10-18.jsonl.gz",
	"real-nuc-fuji-timed-2026-10-15.jsonl.gz",
	"real-nuc-suzuka-timed-race-2026-10-16.jsonl.gz",
	"real-nuc-suzuka-joined-mid-race-2026-10-18.jsonl.gz",
	"real-nuc-sugo-replay-2026-10-18.jsonl.gz",
	"real-nuc-laguna-seca-demo-2026-10-18.jsonl.gz",
}

// scriptOff is the rules file both sides speak with: v1's bundled script off, so
// each callout has one phrase and nothing is picked at random (paddock#64's
// triage: v1 seeds nothing, so this is its only deterministic output). v1 reads it
// as GT7_RULES_FILE, radio as TWR_RADIO_VOICE_FILE, in the same format.
const scriptOff = "script: false\n"

// Line is one callout as a side spoke it.
type Line struct {
	Rule string `json:"rule"`
	Text string `json:"text"`
	// At is when the callout was created, in seconds from the recording's first
	// frame: v1's own time less the first frame's, and radio's clock time, which
	// is ecu replay's 2026-01-01T00:00:00Z plus the same.
	At float64 `json:"at"`
	// Kind is the phrase key radio shows; v1's console has none.
	Kind string `json:"kind,omitempty"`
}

// Speaker is a side that speaks a recording's callouts (paddock#64 R1).
type Speaker interface {
	Images(ctx context.Context) ([]string, error)
	Spoken(ctx context.Context, rec Recording) ([]Line, error)
}

// SpokenDiff is a callout one side said and the other didn't, or said otherwise:
// V1 or V2 is nil on the side that said nothing in its place.
type SpokenDiff struct {
	V1, V2 *Line
	// N1 and N2 are the line's place in each side's list, from 1; 0 for none.
	N1, N2 int
	// Frame is the recording's frame the callout was created on, from 1.
	Frame int
	// Known is the known difference that allows it, by id.
	Known string
}

// Rule is the rule the difference is about.
func (d SpokenDiff) Rule() string {
	if d.V1 != nil {
		return d.V1.Rule
	}
	return d.V2.Rule
}

// SpokenResult is one recording's comparison.
type SpokenResult struct {
	Recording Recording
	V1, V2    []Line
	Matched   int
	Diffs     []SpokenDiff
}

// Failing is the number of differences no known entry allows.
func (r SpokenResult) Failing() int {
	n := 0
	for _, d := range r.Diffs {
		if d.Known == "" {
			n++
		}
	}
	return n
}

func spoken(ctx context.Context, args []string, stdout, stderr io.Writer, mk sides) int {
	var o options
	fs, root, code := commonFlags("spoken", &o, stderr)
	if fs == nil {
		return code
	}
	fs.BoolVar(&o.keep, "keep", false, "keep both sides' spoken lines, and say where")
	all := fs.Bool("all", false, "every recording in recordings/SOURCES.yaml")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: parity spoken [--v1-image IMG] [--compose FILE] [--healthz URL] [--keep] [--all] RECORDING…")
		_, _ = fmt.Fprintln(stderr, "RECORDING is a path, or a file name in recordings/; with none, the nine paddock#64 names.")
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
	names := fs.Args()
	switch {
	case *all && len(names) > 0:
		_, _ = fmt.Fprintln(stderr, "parity: --all takes no RECORDING")
		return exitRefused
	case *all:
		for name := range sums {
			names = append(names, name)
		}
		sort.Strings(names)
	case len(names) == 0:
		names = brundleRecordings
	}
	recs, err := resolveRecordings(names, dir, sums)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}
	a, b := mk(o)
	v1, ok1 := a.(Speaker)
	v2, ok2 := b.(Speaker)
	if !ok1 || !ok2 {
		_, _ = fmt.Fprintln(stderr, "parity: a side that speaks no callouts")
		return exitRefused
	}
	return compareSpokenAll(ctx, recs, v1, v2, o, stdout, stderr)
}

// compareSpokenAll speaks every recording on both sides, prints each one's
// differences and then the table.
func compareSpokenAll(ctx context.Context, recs []Recording, v1, v2 Speaker, o options, stdout, stderr io.Writer) int {
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
		if keepDir, err = os.MkdirTemp("", "parity-spoken-"); err != nil {
			_, _ = fmt.Fprintln(stderr, "parity:", err)
			return exitRefused
		}
	}
	var results []SpokenResult
	for _, rec := range recs {
		a, err := v1.Spoken(ctx, rec)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "parity: %s: v1: %v\n", rec.Name, err)
			return exitRefused
		}
		b, err := v2.Spoken(ctx, rec)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "parity: %s: v2: %v\n", rec.Name, err)
			return exitRefused
		}
		r := compareSpoken(rec, a, b, knownSpoken)
		if len(r.Diffs) > 0 {
			ts, err := frameTimes(rec.Path)
			if err != nil {
				_, _ = fmt.Fprintf(stderr, "parity: %s: %v\n", rec.Name, err)
				return exitRefused
			}
			for i := range r.Diffs {
				r.Diffs[i].Frame = frameAt(ts, r.Diffs[i])
			}
		}
		results = append(results, r)
		if err := writeSpokenResult(stdout, r); err != nil {
			_, _ = fmt.Fprintln(stderr, "parity:", err)
			return exitRefused
		}
		if keepDir != "" {
			if err := keepSpoken(keepDir, rec, a, b); err != nil {
				_, _ = fmt.Fprintln(stderr, "parity:", err)
				return exitRefused
			}
		}
	}
	if err := writeSpokenTable(stdout, results); err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}
	stale := unusedKnown(results, knownSpoken)
	for _, k := range stale {
		_, _ = fmt.Fprintf(stdout, "known.go: %s (%s) on %s matched no difference: remove it\n", k.ID, k.Issue, k.Recording)
	}
	if keepDir != "" {
		_, _ = fmt.Fprintf(stdout, "spoken lines kept in %s\n", keepDir)
	}
	for _, r := range results {
		if r.Failing() > 0 {
			return exitDiffer
		}
	}
	if len(stale) > 0 {
		return exitDiffer
	}
	return exitMatch
}

// compareSpoken aligns both sides' lists on rule and text, in order, and lists
// what is left: a line on one side only, or, where both sides said a line of the
// same rule in the same place, the two texts side by side (R1, R3).
func compareSpoken(rec Recording, v1, v2 []Line, known []knownDifference) SpokenResult {
	r := SpokenResult{Recording: rec, V1: v1, V2: v2}
	same := func(i, j int) bool { return v1[i].Rule == v2[j].Rule && v1[i].Text == v2[j].Text }
	// The longest common subsequence, from the end, so the walk below goes forwards.
	n, m := len(v1), len(v2)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case same(i, j):
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var only1, only2 []int
	flush := func() {
		r.Diffs = append(r.Diffs, pairHunk(v1, v2, only1, only2)...)
		only1, only2 = nil, nil
	}
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && same(i, j):
			flush()
			r.Matched++
			i, j = i+1, j+1
		case j == m || (i < n && lcs[i+1][j] >= lcs[i][j+1]):
			only1 = append(only1, i)
			i++
		default:
			only2 = append(only2, j)
			j++
		}
	}
	flush()
	for k := range r.Diffs {
		r.Diffs[k].Known = knownFor(rec, r.Diffs[k], known)
	}
	return r
}

// pairHunk turns a stretch of lines on one side only into differences: a v1 line
// and a v2 line of the same rule pair up, in order, as one said otherwise; the
// rest stand alone, in the order they were said.
func pairHunk(v1, v2 []Line, only1, only2 []int) []SpokenDiff {
	var diffs []SpokenDiff
	used := map[int]bool{}
	for _, i := range only1 {
		d := SpokenDiff{V1: &v1[i], N1: i + 1}
		for _, j := range only2 {
			if !used[j] && v2[j].Rule == v1[i].Rule {
				used[j] = true
				d.V2, d.N2 = &v2[j], j+1
				break
			}
		}
		diffs = append(diffs, d)
	}
	for _, j := range only2 {
		if !used[j] {
			diffs = append(diffs, SpokenDiff{V2: &v2[j], N2: j + 1})
		}
	}
	sort.SliceStable(diffs, func(a, b int) bool { return diffs[a].at() < diffs[b].at() })
	return diffs
}

// at is when the difference's callout was created: v1's time where it has one.
func (d SpokenDiff) at() float64 {
	if d.V1 != nil {
		return d.V1.At
	}
	return d.V2.At
}

// knownFor is the id of the known difference that allows d, or "".
func knownFor(rec Recording, d SpokenDiff, known []knownDifference) string {
	text := func(l *Line) string {
		if l == nil {
			return ""
		}
		return l.Text
	}
	for _, k := range known {
		if k.Recording == rec.Stem() && k.Rule == d.Rule() && k.V1 == text(d.V1) && k.V2 == text(d.V2) {
			return k.ID
		}
	}
	return ""
}

// unusedKnown are the known differences for a recording that was compared which
// allowed nothing: stale, so the run fails until they are removed.
func unusedKnown(results []SpokenResult, known []knownDifference) []knownDifference {
	ran, used := map[string]bool{}, map[string]bool{}
	for _, r := range results {
		ran[r.Recording.Stem()] = true
		for _, d := range r.Diffs {
			used[d.Known] = true
		}
	}
	var stale []knownDifference
	for _, k := range known {
		if ran[k.Recording] && !used[k.ID] {
			stale = append(stale, k)
		}
	}
	return stale
}

// writeSpokenResult prints a recording's differences: the frame, the rule and
// what each side said there.
func writeSpokenResult(w io.Writer, r SpokenResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s  sha256 %s\n", r.Recording.Name, r.Recording.SHA256)
	if len(r.Diffs) > 0 {
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintf(tw, "\tframe\tat\trule\tv1\tv2\t\n")
		for _, d := range r.Diffs {
			note := ""
			if d.Known != "" {
				note = "known: " + d.Known
			}
			_, _ = fmt.Fprintf(tw, "\t%d\t%.2f s\t%s\t%s\t%s\t%s\n", d.Frame, d.at(), d.Rule(), said(d.V1, d.N1), said(d.V2, d.N2), note)
		}
		_ = tw.Flush()
	}
	if n := r.Failing(); n > 0 {
		fmt.Fprintf(&b, "  differ: %d %s\n", n, plural(n, "difference", "differences"))
	} else {
		fmt.Fprintf(&b, "  match: %d %s\n", r.Matched, plural(r.Matched, "callout", "callouts"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// said is a line as the output shows it: its place and its text, quoted, or "-".
func said(l *Line, n int) string {
	if l == nil {
		return "-"
	}
	return fmt.Sprintf("#%d %q", n, l.Text)
}

// writeSpokenTable prints a line per recording: what each side said, how much of
// it matched, and the differences, failing and known.
func writeSpokenTable(w io.Writer, results []SpokenResult) error {
	var b strings.Builder
	b.WriteString("\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "recording\tv1 said\tv2 said\tmatched\tdifferences\tknown\t\n")
	matched := 0
	for _, r := range results {
		failing := r.Failing()
		if failing == 0 {
			matched++
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t\n", r.Recording.Stem(), len(r.V1), len(r.V2), r.Matched, failing, len(r.Diffs)-failing)
	}
	_ = tw.Flush()
	fmt.Fprintf(&b, "\nparity spoken: %d of %d %s match\n", matched, len(results), plural(len(results), "recording", "recordings"))
	_, err := io.WriteString(w, b.String())
	return err
}

// keepSpoken writes both sides' spoken lines, one JSON object per line.
func keepSpoken(dir string, rec Recording, v1, v2 []Line) error {
	for side, lines := range map[string][]Line{"v1": v1, "v2": v2} {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		for _, l := range lines {
			if err := enc.Encode(l); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, rec.Stem()+".spoken."+side+".jsonl"), []byte(b.String()), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// frameTimes are a recording's frames' own times, `t`, in order.
func frameTimes(path string) ([]float64, error) {
	f, err := os.Open(path) //nolint:gosec // a recording the caller named, its sha256 checked
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		r = gz
	}
	var ts []float64
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var frame struct {
				T *float64 `json:"t"`
			}
			if jerr := json.Unmarshal(line, &frame); jerr != nil || frame.T == nil {
				return nil, fmt.Errorf("frame %d: no t", len(ts)+1)
			}
			ts = append(ts, *frame.T)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if len(ts) == 0 {
		return nil, errors.New("no frames")
	}
	return ts, nil
}

// frameAt is the first frame, from 1, whose time from the first frame reaches
// the difference's: the frame its callout was created on. Both consoles round to
// 10 ms, so a frame within 5 ms counts.
func frameAt(ts []float64, d SpokenDiff) int {
	at := d.at()
	for i, t := range ts {
		if t-ts[0] >= at-0.005 {
			return i + 1
		}
	}
	return len(ts)
}

// v1Line is a line v1's ConsolePlayer prints: `[t s] (rule) text`, t the
// recording's own time. v1 logs to the same output, so other lines are skipped.
var v1Line = regexp.MustCompile(`^\[\s*(-?\d+(?:\.\d+)?)s\] \(([^)]*)\) (.*)$`)

// readV1Lines reads v1 replay's output, its times made relative to t0, the
// recording's first frame's.
func readV1Lines(out []byte, t0 float64) []Line {
	var lines []Line
	for _, raw := range strings.Split(string(out), "\n") {
		m := v1Line.FindStringSubmatch(strings.TrimRight(raw, "\r"))
		if m == nil {
			continue
		}
		t, _ := strconv.ParseFloat(m[1], 64) // the pattern takes only a number
		lines = append(lines, Line{Rule: m[2], Text: m[3], At: math.Round((t-t0)*100) / 100})
	}
	return lines
}

// radioLine is a line radio's console sink prints: v1's ConsolePlayer line with
// the time of day in UTC, then the kind and the callout's id (radio's sink.line).
var radioLine = regexp.MustCompile(`^\[(\d{2}):(\d{2}):(\d{2}(?:\.\d+)?)\] \(([^)]*)\) (.*?)  kind=(\S*) id=(\S*)$`)

// readRadioLines reads radio's log. ecu replay dates the first frame
// 2026-01-01T00:00:00Z, so the time of day is the time from the first frame. A
// line said twice, by the same id, is kept twice: radio must never do it.
func readRadioLines(out []byte) []Line {
	var lines []Line
	for _, raw := range strings.Split(string(out), "\n") {
		m := radioLine.FindStringSubmatch(strings.TrimRight(raw, "\r"))
		if m == nil {
			continue
		}
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		s, _ := strconv.ParseFloat(m[3], 64)
		lines = append(lines, Line{Rule: m[4], Text: m[5], At: math.Round((float64(h*3600+mi*60)+s)*100) / 100, Kind: m[6]})
	}
	return lines
}

// rulesDir makes a directory holding scriptOff as script-false.yaml, which both
// containers mount. Their users aren't the caller, so both are world-readable.
func rulesDir() (string, error) {
	dir, err := os.MkdirTemp("", "parity-spoken-rules-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // read by the containers' own users
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "script-false.yaml"), []byte(scriptOff), 0o644); err != nil { //nolint:gosec // read by the containers' own users
		return "", err
	}
	return dir, nil
}

// Spoken replays a recording with v1's own `replay` and its ConsolePlayer, with the
// script off (R1): the lines v1 would have said, in order.
func (s *v1Side) Spoken(ctx context.Context, rec Recording) ([]Line, error) {
	dir, err := rulesDir()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	ts, err := frameTimes(rec.Path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rec.Name, err)
	}
	inside := "/recording/" + rec.Name
	out, err := s.run(ctx, s.container, "run", "--rm",
		"-e", "GT7_RULES_FILE=/rules/script-false.yaml",
		"-v", dir+":/rules:ro,z",
		"-v", rec.Path+":"+inside+":ro,z",
		s.image, "replay", inside)
	if err != nil {
		return nil, fmt.Errorf("v1 replay: %w", err)
	}
	return readV1Lines(out, ts[0]), nil
}

// radioOverride is the compose file laid over the stack's for a spoken run: radio
// on the harness's source, to the console, in instant timing (v1's replay: every
// callout said, in the queue's order, none stale), with the script off.
func radioOverride(rules string) string {
	src, _ := json.Marshal(filepath.Join(rules, "script-false.yaml"))
	return `services:
  radio:
    environment:
      TWR_RADIO_SRC: parity
      TWR_RADIO_SINK: console
      TWR_RADIO_TIMING: instant
      TWR_RADIO_VOICE_FILE: /rules/script-false.yaml
    volumes:
      - type: bind
        source: ` + string(src) + `
        target: /rules/script-false.yaml
        read_only: true
        bind:
          selinux: z
`
}

// radioTaking is radio's log line once its consumers are bound and it follows the
// source's events: from then on, nothing ecu publishes is missed.
const radioTaking = `msg="callouts: taking"`

// radioStopped is in radio's log once it is stopping, whatever the reason.
var radioStopped = regexp.MustCompile(`(?m)^time=\S+ level=\S+ msg=(stopped|"shutting down")`)

// Spoken replays a recording through a fresh stack with radio speaking to its
// console (R1): ecu replay onto the harness's source, radio started before it and
// read until it has said nothing new for said.
func (s *v2Side) Spoken(ctx context.Context, rec Recording) (lines []Line, err error) {
	dir, err := rulesDir()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	override := filepath.Join(dir, "radio.yaml")
	if err := os.WriteFile(override, []byte(radioOverride(dir)), 0o644); err != nil { //nolint:gosec // read by compose, not a secret
		return nil, err
	}
	project := "parity-" + safeName(rec.Stem())
	stack := func(args ...string) []string {
		return append(append(append([]string{}, s.compose...), "-p", project, "-f", s.file, "-f", override), args...)
	}
	down := []string{"--profile", "replay", "down", "--volumes", "--remove-orphans"}
	if _, err := s.run(ctx, stack(down...)...); err != nil {
		return nil, err
	}
	defer func() {
		args := down
		if s.keep {
			args = []string{"--profile", "replay", "stop"}
			_, _ = fmt.Fprintf(s.log, "parity: kept v2's stack, compose project %s (stopped, volumes kept)\n", project)
		}
		if _, downErr := s.run(context.WithoutCancel(ctx), stack(args...)...); downErr != nil && err == nil {
			err = downErr
		}
	}()
	if _, err := s.run(ctx, stack("up", "-d")...); err != nil {
		return nil, fmt.Errorf("v2 up: %w", err)
	}
	if _, err := s.waitLaps(ctx, -1); err != nil {
		return nil, fmt.Errorf("v2: logbook not healthy: %w", err)
	}
	// By profile, as `make radio` starts it: podman compose doesn't enable a
	// profile for a service named on the command line.
	if _, err := s.run(ctx, stack("--profile", "replay", "up", "-d", "--no-deps", "radio")...); err != nil {
		return nil, fmt.Errorf("v2 radio: %w", err)
	}
	logs := stack("--profile", "replay", "logs", "--no-color", "--no-log-prefix", "radio")
	if err := s.waitLog(ctx, logs, func(out []byte) bool { return bytes.Contains(out, []byte(radioTaking)) }); err != nil {
		return nil, fmt.Errorf("v2: radio not taking callouts: %w", err)
	}
	inside := "/recording/" + rec.Name
	if _, err := s.run(ctx, stack("run", "--rm", "--no-deps", "-T",
		"-v", rec.Path+":"+inside+":ro,z",
		"replay", "replay", "--src", "parity", "--speed", "max", inside)...); err != nil {
		return nil, fmt.Errorf("v2 replay: %w", err)
	}
	// ecu has published everything when it exits; radio says the last of it once
	// its replay clock has crept past it, a second later (radio.timing's LAG).
	out, err := s.waitSaid(ctx, logs)
	if err != nil {
		return nil, fmt.Errorf("v2: radio: %w", err)
	}
	if radioStopped.Match(out) {
		return nil, fmt.Errorf("v2: radio stopped during the replay:\n%s", tail(string(out), 20))
	}
	// A radio killed without a word (out of memory, SIGKILL, a native crash)
	// leaves no line in its log, only a list cut short: a silent recording would
	// pass as nothing said on both sides. So it must still be running.
	running, err := s.run(ctx, stack("--profile", "replay", "ps", "--status", "running", "-q", "radio")...)
	if err != nil {
		return nil, fmt.Errorf("v2: radio: %w", err)
	}
	if len(bytes.TrimSpace(running)) == 0 {
		return nil, fmt.Errorf("v2: radio was not running at the end of the replay:\n%s", tail(string(out), 20))
	}
	return readRadioLines(out), nil
}

// waitLog polls a log until done says it holds what is waited for.
func (s *v2Side) waitLog(ctx context.Context, logs []string, done func([]byte) bool) error {
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	for {
		out, err := s.run(ctx, logs...)
		if err == nil && done(out) {
			return nil
		}
		if err == nil && radioStopped.Match(out) {
			return fmt.Errorf("radio stopped:\n%s", tail(string(out), 20))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.poll):
		}
	}
}

// waitSaid returns radio's log once its spoken lines have stayed the same for
// said.
func (s *v2Side) waitSaid(ctx context.Context, logs []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	last, since := -1, time.Now()
	for {
		out, err := s.run(ctx, logs...)
		if err != nil {
			return nil, err
		}
		if n := len(readRadioLines(out)); n != last {
			last, since = n, time.Now()
		} else if time.Since(since) >= s.said {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.poll):
		}
	}
}
