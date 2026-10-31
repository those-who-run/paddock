package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// wallRecordings are #133's three, as the Rosberg plan trims it (the maintainer's
// decision, 2026-10-31): sessions, laps and traces on a race at Circuit Gilles
// Villeneuve, the pit stop at the same track a day later (#122 AC2, #123 AC1 and
// AC2's Gilles half), whose sessions are each other's same-track, and Suzuka
// joined part way (#122 AC3).
var wallRecordings = []string{
	"real-nuc-gilles-villeneuve-race-2026-10-17.jsonl.gz",
	"real-nuc-gilles-villeneuve-pit-stop-2026-10-17.jsonl.gz",
	"real-nuc-suzuka-joined-mid-race-2026-10-18.jsonl.gz",
}

// wallDay is how far apart the recordings' clocks are: the n-th recording's
// first frame is replayClock plus n-1 days on both sides, so their sessions are
// apart in the list and each session's day names its recording.
const wallDay = 24 * time.Hour

// wallClocks are the recordings' clocks, in order.
func wallClocks(n int) []time.Time {
	clocks := make([]time.Time, n)
	for i := range clocks {
		clocks[i] = replayClock.Add(time.Duration(i) * wallDay)
	}
	return clocks
}

// tracePoints is how many points each trace is asked for (#133 R4).
const tracePoints = 1500

// jumpMetres is v1's JUMP_METRES: a pit lap's trace keeps the frames either side
// of the cut-scene, so two points of it are further apart than this (#123 AC2).
const jumpMetres = 20.0

// wallAnswer is one answer of a pit wall's data API in v1's names and shapes:
// call is the route (sessions, session, laps, same_track, trace), and the session
// and lap are what it was asked for.
type wallAnswer struct {
	Call      string         `json:"call"`
	SessionID string         `json:"session_id"`
	Lap       int            `json:"lap"`
	Body      map[string]any `json:"body"`
}

// PitWall is a side whose pit wall answers over a store of the recordings,
// replayed in order, each on its clock (paddock#133).
type PitWall interface {
	WallImages(ctx context.Context) ([]string, error)
	// PitWall replays the recordings and asks the pit wall. want is the number of
	// laps v1 listed, so v2 knows when its store is complete.
	PitWall(ctx context.Context, recs []Recording, clocks []time.Time, want int) ([]wallAnswer, error)
}

// WallDiff is one difference: the route, the recording and the session's place in
// it (from 1, in start order), the lap, the field and both values. A difference
// in the list's own fields (its order, options, total) has no recording.
type WallDiff struct {
	Call     string
	Rec      string
	Session  int
	Lap      int
	Field    string
	V1, V2   string
	Accepted string
}

// WallResult is the comparison of both sides' answers.
type WallResult struct {
	Recs  []Recording
	Diffs []WallDiff
	// Counts per recording: sessions, laps and traces on each side.
	Counts map[string]*wallCount
}

type wallCount struct{ Sessions, Laps, Traces [2]int }

// Failing is the number of differences no accepted entry allows.
func (r WallResult) Failing() int {
	n := 0
	for _, d := range r.Diffs {
		if d.Accepted == "" {
			n++
		}
	}
	return n
}

func pitwall(ctx context.Context, args []string, stdout, stderr io.Writer, mk sides) int {
	var o options
	fs, root, code := commonFlags("pitwall", &o, stderr)
	if fs == nil {
		return code
	}
	fs.StringVar(&o.pitwall, "pitwall", defaultPitwall, "the pit wall's address, as the harness's compose override publishes it")
	fs.BoolVar(&o.keep, "keep", false, "keep v2's stack and both sides' answers, and say where")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: parity pitwall [--v1-image IMG] [--compose FILE] [--healthz URL] [--pitwall URL] [--keep] [RECORDING…]")
		_, _ = fmt.Fprintln(stderr, "RECORDING is a path, or a file name in recordings/; with none, #133's three, replayed onto one store.")
		fs.PrintDefaults()
	}
	if code, ok := parseFlags(fs, args, &o, stderr); !ok {
		return code
	}
	names := fs.Args()
	if len(names) == 0 {
		names = wallRecordings
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
	a, b := mk(o)
	v1, ok1 := a.(PitWall)
	v2, ok2 := b.(PitWall)
	if !ok1 || !ok2 {
		_, _ = fmt.Fprintln(stderr, "parity: a side with no pit wall")
		return exitRefused
	}
	return compareWallAll(ctx, recs, v1, v2, o, stdout, stderr)
}

// compareWallAll runs the recordings through both sides, compares the answers and
// prints the differences and the table.
func compareWallAll(ctx context.Context, recs []Recording, v1, v2 PitWall, o options, stdout, stderr io.Writer) int {
	for i, side := range []PitWall{v1, v2} {
		images, err := side.WallImages(ctx)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "parity: v%d: %v\n", i+1, err)
			return exitRefused
		}
		_, _ = fmt.Fprintf(stdout, "v%d images: %s\n", i+1, strings.Join(images, ", "))
	}
	clocks := wallClocks(len(recs))
	a, err := v1.PitWall(ctx, recs, clocks, 0)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity: v1:", err)
		return exitRefused
	}
	want := 0
	for _, ans := range a {
		if ans.Call == "laps" {
			laps, _ := ans.Body["laps"].([]any)
			want += len(laps)
		}
	}
	// Nothing to compare against: a wrong image would otherwise match a v2 that
	// stored nothing either.
	if want == 0 {
		_, _ = fmt.Fprintln(stderr, "parity: refused: v1's pit wall lists no laps")
		return exitRefused
	}
	b, err := v2.PitWall(ctx, recs, clocks, want)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "parity: v2:", err)
		return exitRefused
	}
	r := compareWall(recs, a, b)
	stale := acceptWall(r.Diffs, wallAccepted)
	if err := writeWallResult(stdout, r); err != nil {
		_, _ = fmt.Fprintln(stderr, "parity:", err)
		return exitRefused
	}
	for _, e := range stale {
		_, _ = fmt.Fprintf(stdout, "accepted.go: %s (%s) allowed no difference: remove it\n", e.ID, e.Issue)
	}
	if o.keep {
		dir, err := keepWall(a, b)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "parity:", err)
			return exitRefused
		}
		_, _ = fmt.Fprintf(stdout, "answers kept in %s\n", dir)
	}
	if r.Failing() > 0 || len(stale) > 0 {
		return exitDiffer
	}
	return exitMatch
}

// readWallAnswers reads JSON lines, one answer each.
func readWallAnswers(b []byte) ([]wallAnswer, error) {
	var out []wallAnswer
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var a wallAnswer
		if err := json.Unmarshal(line, &a); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if a.Call == "" || a.Body == nil {
			return nil, fmt.Errorf("line %d: no call or no body", n)
		}
		out = append(out, a)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// wallSide is one side's answers, indexed.
type wallSide struct {
	list     map[string]any
	sessions map[string]map[string]any // by id: the session answer
	laps     map[string][]any
	same     map[string][]any
	traces   map[string]map[int]map[string]any
	// keys are each session's recording and place, by id; ids the reverse.
	keys map[string]sessionKey
	ids  map[sessionKey]string
}

// sessionKey is a session's recording, by its place in the run (0 is the first),
// and its place in that recording, from 1, in start order (R2: each side mints its
// own ids, so sessions pair by when they started).
type sessionKey struct{ Rec, N int }

func indexWall(answers []wallAnswer) (*wallSide, error) {
	s := &wallSide{
		sessions: map[string]map[string]any{}, laps: map[string][]any{}, same: map[string][]any{},
		traces: map[string]map[int]map[string]any{}, keys: map[string]sessionKey{}, ids: map[sessionKey]string{},
	}
	for _, a := range answers {
		switch a.Call {
		case "sessions":
			s.list = a.Body
		case "session":
			s.sessions[a.SessionID] = a.Body
		case "laps":
			s.laps[a.SessionID], _ = a.Body["laps"].([]any)
		case "same_track":
			s.same[a.SessionID], _ = a.Body["sessions"].([]any)
		case "trace":
			if s.traces[a.SessionID] == nil {
				s.traces[a.SessionID] = map[int]map[string]any{}
			}
			s.traces[a.SessionID][a.Lap] = a.Body
		default:
			return nil, fmt.Errorf("an answer to %q", a.Call)
		}
	}
	if s.list == nil {
		return nil, fmt.Errorf("no session list")
	}
	listed, _ := s.list["sessions"].([]any)
	type started struct {
		id string
		at float64
	}
	var all []started
	for _, x := range listed {
		m, _ := x.(map[string]any)
		id, _ := m["session_id"].(string)
		at, ok := m["started_at"].(float64)
		if id == "" || !ok {
			return nil, fmt.Errorf("a listed session with no id or start: %v", show(m))
		}
		all = append(all, started{id, at})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at < all[j].at })
	base := float64(replayClock.Unix())
	for _, st := range all {
		rec := int(math.Floor((st.at - base) / wallDay.Seconds()))
		k := sessionKey{Rec: rec}
		for k.N = 1; s.ids[k] != ""; k.N++ {
		}
		s.keys[st.id], s.ids[k] = k, st.id
	}
	return s, nil
}

// compareWall pairs both sides' sessions and compares every answer (R1–R4).
func compareWall(recs []Recording, v1, v2 []wallAnswer) WallResult {
	r := WallResult{Recs: recs, Counts: map[string]*wallCount{}}
	for _, rec := range recs {
		r.Counts[rec.Stem()] = &wallCount{}
	}
	a, errA := indexWall(v1)
	b, errB := indexWall(v2)
	if errA != nil || errB != nil {
		r.Diffs = append(r.Diffs, WallDiff{Call: "sessions", Field: "answers", V1: errText(errA), V2: errText(errB)})
		return r
	}
	recName := func(k sessionKey) string {
		if k.Rec >= 0 && k.Rec < len(recs) {
			return recs[k.Rec].Stem()
		}
		return "day " + strconv.Itoa(k.Rec)
	}
	add := func(d WallDiff) { r.Diffs = append(r.Diffs, d) }
	ref := func(side *wallSide, x any) string {
		m, _ := x.(map[string]any)
		id, _ := m["session_id"].(string)
		k, ok := side.keys[id]
		if !ok {
			return "unlisted " + id
		}
		return fmt.Sprintf("%s#%d", recName(k), k.N)
	}
	refs := func(side *wallSide, xs []any) string {
		out := make([]string, len(xs))
		for i, x := range xs {
			out[i] = ref(side, x)
		}
		return strings.Join(out, ", ")
	}

	// The list's own fields: its order, newest first, the counts and the options.
	l1, _ := a.list["sessions"].([]any)
	l2, _ := b.list["sessions"].([]any)
	if x, y := refs(a, l1), refs(b, l2); x != y {
		add(WallDiff{Call: "sessions", Field: "order", V1: x, V2: y})
	}
	for _, f := range []string{"total", "matched"} {
		if !valueEqual(f, a.list[f], b.list[f]) {
			add(WallDiff{Call: "sessions", Field: f, V1: show(a.list[f]), V2: show(b.list[f])})
		}
	}
	o1, _ := a.list["options"].(map[string]any)
	o2, _ := b.list["options"].(map[string]any)
	for _, f := range []string{"track", "car", "kind"} {
		if x, y := show(o1[f]), show(o2[f]); x != y {
			add(WallDiff{Call: "sessions", Field: "options." + f, V1: x, V2: y})
		}
	}

	keys := map[sessionKey]bool{}
	for k := range a.ids {
		keys[k] = true
	}
	for k := range b.ids {
		keys[k] = true
	}
	ordered := make([]sessionKey, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Rec != ordered[j].Rec {
			return ordered[i].Rec < ordered[j].Rec
		}
		return ordered[i].N < ordered[j].N
	})
	listed := func(list []any, id string) map[string]any {
		for _, x := range list {
			if m, _ := x.(map[string]any); m["session_id"] == id {
				return m
			}
		}
		return nil
	}
	for _, k := range ordered {
		rec := recName(k)
		id1, id2 := a.ids[k], b.ids[k]
		c := r.Counts[rec]
		if c == nil {
			c = &wallCount{}
			r.Counts[rec] = c
		}
		if id1 != "" {
			c.Sessions[0]++
			c.Laps[0] += len(a.laps[id1])
			c.Traces[0] += len(a.traces[id1])
		}
		if id2 != "" {
			c.Sessions[1]++
			c.Laps[1] += len(b.laps[id2])
			c.Traces[1] += len(b.traces[id2])
		}
		at := WallDiff{Rec: rec, Session: k.N}
		if id1 == "" || id2 == "" {
			d := at
			d.Call, d.Field, d.V1, d.V2 = "sessions", "session", stored(btoi(id1 != "")), stored(btoi(id2 != ""))
			add(d)
			continue
		}
		for _, call := range []string{"sessions", "session"} {
			x, y := listed(l1, id1), listed(l2, id2)
			if call == "session" {
				x, y = a.sessions[id1], b.sessions[id2]
			}
			for _, fd := range diffFields(x, y) {
				d := at
				d.Call, d.Field, d.V1, d.V2 = call, fd.field, fd.v1, fd.v2
				add(d)
			}
		}
		if x, y := refs(a, a.same[id1]), refs(b, b.same[id2]); x != y {
			d := at
			d.Call, d.Field, d.V1, d.V2 = "same_track", "sessions", x, y
			add(d)
		}
		for _, d := range compareLaps(a.laps[id1], b.laps[id2]) {
			d.Rec, d.Session = rec, k.N
			add(d)
		}
		for _, d := range compareTraces(a, b, id1, id2) {
			d.Rec, d.Session = rec, k.N
			add(d)
		}
	}
	return r
}

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// fieldDiff is one field that differs, by its path in the answer.
type fieldDiff struct{ field, v1, v2 string }

// wallNotCompared are the fields left out of the comparison: the session's id, which
// each side mints (R2; sessions pair by start), and v1's lap file, logbook's own
// layout, which contracts left out (contracts' pitwall_v1_test.go).
var wallNotCompared = map[string]bool{"session_id": true, "file": true}

// wallTolerance is each field's tolerance (R3): times to 1 s, v1's Unix seconds
// against ecu's milliseconds, and fuel to 0.001 L as `parity laps` compares it.
// Every other value is compared exactly, a trace's values among them (R4).
var wallTolerance = map[string]float64{
	"started_at": 1, "ended_at": 1,
	"fuel_start": 0.001, "fuel_end": 0.001, "fuel_used": 0.001, "fuel_capacity": 0.001, "fuel_by_stint": 0.001,
}

// diffFields compares two answers' fields, nested objects by path (track.name).
func diffFields(x, y map[string]any) []fieldDiff {
	var out []fieldDiff
	var walk func(prefix string, x, y map[string]any)
	walk = func(prefix string, x, y map[string]any) {
		names := map[string]bool{}
		for k := range x {
			names[k] = true
		}
		for k := range y {
			names[k] = true
		}
		sorted := make([]string, 0, len(names))
		for k := range names {
			if !wallNotCompared[k] {
				sorted = append(sorted, k)
			}
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			a, b := x[k], y[k]
			ma, oka := a.(map[string]any)
			mb, okb := b.(map[string]any)
			if oka && okb {
				walk(prefix+k+".", ma, mb)
				continue
			}
			if !wallEqual(k, a, b) {
				out = append(out, fieldDiff{prefix + k, show(a), show(b)})
			}
		}
	}
	walk("", x, y)
	return out
}

// wallEqual compares a field's values within its tolerance; a list compares
// value for value.
func wallEqual(field string, a, b any) bool {
	la, oka := a.([]any)
	lb, okb := b.([]any)
	if oka && okb {
		if len(la) != len(lb) {
			return false
		}
		for i := range la {
			if !wallEqual(field, la[i], lb[i]) {
				return false
			}
		}
		return true
	}
	x, okx := a.(float64)
	y, oky := b.(float64)
	if okx && oky {
		return math.Abs(x-y) <= wallTolerance[field]+1e-9
	}
	return reflect.DeepEqual(a, b)
}

// compareLaps pairs two lap tables by lap number.
func compareLaps(x, y []any) []WallDiff {
	byLap := func(laps []any) (map[int]map[string]any, []int) {
		m := map[int]map[string]any{}
		var order []int
		for _, l := range laps {
			lap, _ := l.(map[string]any)
			n, _ := lap["lap"].(float64)
			if _, dup := m[int(n)]; !dup {
				order = append(order, int(n))
			}
			m[int(n)] = lap
		}
		return m, order
	}
	a, oa := byLap(x)
	b, ob := byLap(y)
	nums := append(append([]int{}, oa...), ob...)
	sort.Ints(nums)
	nums = compactInts(nums)
	var out []WallDiff
	for _, n := range nums {
		la, lb := a[n], b[n]
		if la == nil || lb == nil {
			out = append(out, WallDiff{Call: "laps", Lap: n, Field: "lap", V1: stored(btoi(la != nil)), V2: stored(btoi(lb != nil))})
			continue
		}
		for _, fd := range diffFields(la, lb) {
			out = append(out, WallDiff{Call: "laps", Lap: n, Field: fd.field, V1: fd.v1, V2: fd.v2})
		}
	}
	return out
}

func compactInts(s []int) []int {
	out := s[:0]
	for i, n := range s {
		if i == 0 || n != s[i-1] {
			out = append(out, n)
		}
	}
	return out
}

// traceChannels are a trace's arrays, in v1's order.
var traceChannels = []string{"t", "speed_kph", "throttle_pct", "brake_pct", "gear", "rpm", "x", "z"}

// compareTraces compares every trace either side asked for, value for value
// (R4), and checks that a pit lap's trace on v2 keeps its jump (#123 AC2): v1's
// laps say which laps are pit laps.
func compareTraces(a, b *wallSide, id1, id2 string) []WallDiff {
	laps := map[int]bool{}
	for n := range a.traces[id1] {
		laps[n] = true
	}
	for n := range b.traces[id2] {
		laps[n] = true
	}
	nums := make([]int, 0, len(laps))
	for n := range laps {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	pit := map[int]bool{}
	for _, l := range a.laps[id1] {
		lap, _ := l.(map[string]any)
		if n, ok := lap["lap"].(float64); ok && lap["pit"] == true {
			pit[int(n)] = true
		}
	}
	var out []WallDiff
	for _, n := range nums {
		x, y := a.traces[id1][n], b.traces[id2][n]
		if x == nil || y == nil {
			out = append(out, WallDiff{Call: "trace", Lap: n, Field: "trace", V1: asked(x != nil), V2: asked(y != nil)})
			continue
		}
		if x["lap"] != y["lap"] {
			out = append(out, WallDiff{Call: "trace", Lap: n, Field: "lap", V1: show(x["lap"]), V2: show(y["lap"])})
		}
		for _, ch := range traceChannels {
			if d, ok := channelDiff(x[ch], y[ch]); !ok {
				out = append(out, WallDiff{Call: "trace", Lap: n, Field: ch, V1: d[0], V2: d[1]})
			}
		}
		if pit[n] {
			if jump := longestStep(y); jump <= jumpMetres {
				out = append(out, WallDiff{Call: "trace", Lap: n, Field: "jump", V1: fmt.Sprintf("a pit lap, over %.0f m", jumpMetres), V2: fmt.Sprintf("%.1f m at most", jump)})
			}
		}
	}
	return out
}

func asked(ok bool) string {
	if ok {
		return "traced"
	}
	return "not traced"
}

// channelDiff compares one channel value for value. A difference says how many
// values differ and the first of them, on each side.
func channelDiff(x, y any) ([2]string, bool) {
	a, _ := x.([]any)
	b, _ := y.([]any)
	if len(a) != len(b) {
		return [2]string{fmt.Sprintf("%d points", len(a)), fmt.Sprintf("%d points", len(b))}, false
	}
	n, first := 0, -1
	for i := range a {
		if !wallEqual("", a[i], b[i]) {
			n++
			if first < 0 {
				first = i
			}
		}
	}
	if n == 0 {
		return [2]string{}, true
	}
	return [2]string{
		fmt.Sprintf("%d of %d differ, first [%d] %s", n, len(a), first, show(a[first])),
		show(b[first]),
	}, false
}

// longestStep is the longest step in position between two points of a trace, in
// metres.
func longestStep(trace map[string]any) float64 {
	xs, _ := trace["x"].([]any)
	zs, _ := trace["z"].([]any)
	longest := 0.0
	for i := 1; i < len(xs) && i < len(zs); i++ {
		x0, _ := xs[i-1].(float64)
		x1, _ := xs[i].(float64)
		z0, _ := zs[i-1].(float64)
		z1, _ := zs[i].(float64)
		longest = max(longest, math.Hypot(x1-x0, z1-z0))
	}
	return longest
}

// writeWallResult prints every difference, a line each, then a line per
// recording and the verdict.
func writeWallResult(w io.Writer, r WallResult) error {
	var b strings.Builder
	if len(r.Diffs) > 0 {
		b.WriteString("\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintf(tw, "recording\tsession\tcall\tlap\tfield\tv1\tv2\t\n")
		for _, d := range r.Diffs {
			rec, session, lap, note := d.Rec, "", "", ""
			if rec == "" {
				rec = "-"
			} else {
				session = "#" + strconv.Itoa(d.Session)
			}
			if d.Lap > 0 {
				lap = strconv.Itoa(d.Lap)
			}
			if d.Accepted != "" {
				note = "accepted: " + d.Accepted
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", rec, session, d.Call, lap, d.Field, d.V1, d.V2, note)
		}
		_ = tw.Flush()
	}
	b.WriteString("\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "recording\tsessions v1\tv2\tlaps v1\tv2\ttraces v1\tv2\tdifferences\taccepted\t\n")
	per := map[string][2]int{}
	for _, d := range r.Diffs {
		n := per[d.Rec]
		if d.Accepted == "" {
			n[0]++
		} else {
			n[1]++
		}
		per[d.Rec] = n
	}
	for _, rec := range r.Recs {
		c := r.Counts[rec.Stem()]
		n := per[rec.Stem()]
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t\n", rec.Stem(), c.Sessions[0], c.Sessions[1], c.Laps[0], c.Laps[1], c.Traces[0], c.Traces[1], n[0], n[1])
	}
	if n := per[""]; n != [2]int{} {
		_, _ = fmt.Fprintf(tw, "the list\t\t\t\t\t\t\t%d\t%d\t\n", n[0], n[1])
	}
	_ = tw.Flush()
	if n := r.Failing(); n > 0 {
		fmt.Fprintf(&b, "\nparity pitwall: %d %s\n", n, plural(n, "difference", "differences"))
	} else {
		fmt.Fprintf(&b, "\nparity pitwall: match, %d accepted %s\n", len(r.Diffs), plural(len(r.Diffs), "difference", "differences"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// keepWall writes both sides' answers, one JSON object per line, into a new
// directory.
func keepWall(v1, v2 []wallAnswer) (string, error) {
	dir, err := os.MkdirTemp("", "parity-pitwall-")
	if err != nil {
		return "", err
	}
	for side, answers := range map[string][]wallAnswer{"v1": v1, "v2": v2} {
		var b strings.Builder
		enc := json.NewEncoder(&b)
		for _, a := range answers {
			if err := enc.Encode(a); err != nil {
				return "", err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "pitwall."+side+".jsonl"), []byte(b.String()), 0o600); err != nil {
			return "", err
		}
	}
	return dir, nil
}
