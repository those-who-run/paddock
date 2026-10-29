package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// testdata/run holds one run of the pit stop recording as each side gave it, made
// with v1events.py in v1 1.0.0's image and with ecu 0.1.6's replay heard on a NATS
// of its own: the real items, normalised, so the tests compare what the sides give.
func runItems(t *testing.T, side string) []Item {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "run", "real-nuc-suzuka-pit-stop-2026-10-15."+side+".jsonl")) //nolint:gosec // the test's own testdata
	if err != nil {
		t.Fatal(err)
	}
	items, err := readItems(b)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

var pitRun = Run{Recordings: []Recording{pitRec}}

func failing(r EventResult) []EventDiff {
	var out []EventDiff
	for _, d := range r.Diffs {
		if d.Accepted == "" {
			out = append(out, d)
		}
	}
	return out
}

// The pit stop through v1 and through ecu 0.1.6: every event matches, and what is
// left is accepted (pit roles and callouts, which ecu does not publish yet).
func TestTheRealPitStopRunMatches(t *testing.T) {
	r := compareItems(pitRun, runItems(t, "v1"), runItems(t, "v2"))
	if f := failing(r); len(f) > 0 {
		t.Fatalf("%+v", f)
	}
	if r.Events.V1 != 15 || r.Events.V2 != 15 || r.Callouts.V1 != 15 || r.Callouts.V2 != 0 {
		t.Errorf("events %+v callouts %+v", r.Events, r.Callouts)
	}
	ids := map[string]int{}
	for _, d := range r.Diffs {
		ids[d.Accepted]++
	}
	if ids["pending-pit-role"] != 6 || ids["pending-callouts"] != 15 || len(ids) != 2 {
		t.Errorf("accepted %v", ids)
	}
}

// A changed decision fails and names the item, its frame and the field.
func TestAChangedPitEntryFails(t *testing.T) {
	v2 := runItems(t, "v2")
	for i, it := range v2 {
		if it.Type == "pit.entered" {
			v2[i].Fields["in_lap"] = 4.0
		}
	}
	r := compareItems(pitRun, runItems(t, "v1"), v2)
	f := failing(r)
	if len(f) != 1 || f[0].Label != "pit.entered" || f[0].At != "455" || f[0].Field != "in_lap" || f[0].V1 != "3" || f[0].V2 != "4" {
		t.Fatalf("%+v", f)
	}
}

// A frame off by one is two items, one on each side, with a tolerance of zero
// (R2); within a stated tolerance it is one, with both frames shown.
func TestFrameTolerance(t *testing.T) {
	v2 := runItems(t, "v2")
	for i, it := range v2 {
		if it.Type == "race.finished" {
			v2[i].At = at(844)
		}
	}
	r := compareItems(pitRun, runItems(t, "v1"), v2)
	f := failing(r)
	if len(f) != 2 || f[0].V2 != "missing" || f[1].V1 != "missing" || f[0].At != "843" || f[1].At != "844" {
		t.Fatalf("%+v", f)
	}
	frameTolerance["race.finished"] = 1
	defer delete(frameTolerance, "race.finished")
	r = compareItems(pitRun, runItems(t, "v1"), v2)
	if f := failing(r); len(f) != 0 {
		t.Fatalf("%+v", f)
	}
	// A field difference on a pair matched within the tolerance shows both frames.
	for i, it := range v2 {
		if it.Type == "race.finished" {
			v2[i].Fields["lap"] = 7.0
		}
	}
	r = compareItems(pitRun, runItems(t, "v1"), v2)
	if f := failing(r); len(f) != 1 || f[0].At != "843/844" || f[0].Field != "lap" {
		t.Fatalf("%+v", f)
	}
}

// lapTimeCallout is contracts' lap time example as a side's item.
func lapTimeCallout(t *testing.T) Item {
	t.Helper()
	it, err := v2Item(busMsg{Subject: "twr.v1.parity.callout.p2", Data: example(t, "callout_lap_time")}, map[string]int{"01JB8Z3K5Q9T2M7XW4R6N1PDHE": 1})
	if err != nil {
		t.Fatal(err)
	}
	it.Part = 1
	return it
}

func example(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "contracts", name+".json")) //nolint:gosec // the test's own testdata
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// clone copies an item with its fields, so a test can change one side.
func cloneItem(it Item) Item {
	fields := map[string]any{}
	for k, v := range it.Fields {
		if m, ok := v.(map[string]any); ok {
			c := map[string]any{}
			for k2, v2 := range m {
				c[k2] = v2
			}
			v = c
		}
		fields[k] = v
	}
	it.Fields = fields
	return it
}

// Contracts' callout examples decode to rule, kind, slots, priority and the pit
// lane; the text is carried but not compared (the triage of #104).
func TestCalloutExamplesNormalise(t *testing.T) {
	it := lapTimeCallout(t)
	if it.Type != "callout" || it.Session != 1 || *it.At != 19236 {
		t.Fatalf("%+v", it)
	}
	want := map[string]string{
		"rule": "lap_time", "kind": "delta_slower", "priority": "20", "pit_lane": "hold",
		"slots": `{"delta_ms":1000,"lap":3,"lap_time_ms":99000}`,
	}
	for k, v := range want {
		if show(it.Fields[k]) != v {
			t.Errorf("%s = %s, want %s", k, show(it.Fields[k]), v)
		}
	}
	for name, kind := range map[string]string{
		"callout_new_best": "template_new_best", "callout_position": "template_lost",
		"callout_race_summary": "template_position_podium", "callout_start_brief": "template",
		"callout_telemetry": "template_lost",
	} {
		it, err := v2Item(busMsg{Subject: "twr.v1.parity.callout.p1", Data: example(t, name)}, map[string]int{})
		if err != nil || it.Fields["kind"] != kind {
			t.Errorf("%s: %v %+v", name, err, it)
		}
	}
}

// AC2: a rule with its lap delta 0.1 s off fails the diff, naming the lap and the
// frame; the same callout matches itself.
func TestALapDeltaOffByATenthFails(t *testing.T) {
	v1 := lapTimeCallout(t)
	v2 := cloneItem(v1)
	if r := compareItems(pitRun, []Item{v1}, []Item{v2}); len(r.Diffs) != 0 || r.Callouts.Matched != 1 {
		t.Fatalf("%+v", r)
	}
	v2.Fields["slots"].(map[string]any)["delta_ms"] = 1100.0
	r := compareItems(pitRun, []Item{v1}, []Item{v2})
	f := failing(r)
	if len(f) != 1 || f[0].Field != "slots" || f[0].At != "19236" || f[0].Label != "callout lap_time" {
		t.Fatalf("%+v", f)
	}
	var out bytes.Buffer
	if err := writeEventResult(&out, r); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`19236\s+1\s+callout lap_time\s+slots\s+\{"delta_ms":1000,"lap":3,`).MatchString(out.String()) ||
		!strings.Contains(out.String(), "differ: 1 difference") {
		t.Errorf("%s", out.String())
	}
}

// v1's slots are null until a rule's port adds them: then slots are not compared,
// and the rest still is.
func TestNullV1SlotsAreNotCompared(t *testing.T) {
	v2 := lapTimeCallout(t)
	v1 := cloneItem(v2)
	v1.Fields["slots"] = nil
	if r := compareItems(pitRun, []Item{v1}, []Item{v2}); len(r.Diffs) != 0 {
		t.Fatalf("%+v", r.Diffs)
	}
	v1.Fields["priority"] = 11.0
	if f := failing(compareItems(pitRun, []Item{v1}, []Item{v2})); len(f) != 1 || f[0].Field != "priority" {
		t.Fatalf("%+v", f)
	}
}

// Once v2 publishes any callout, a callout missing from v2 fails: pending-callouts
// is only for a run with none.
func TestPendingCalloutsEndWithTheFirstCallout(t *testing.T) {
	v1 := runItems(t, "v1")
	v2 := append(runItems(t, "v2"), lapTimeCallout(t))
	r := compareItems(pitRun, v1, v2)
	if f := failing(r); len(f) != 16 { // v1's 15, and the example on v2 only
		t.Fatalf("%d failing: %+v", len(f), f)
	}
}

// Two callouts of one rule on one frame pair by kind, whichever came first.
func TestCalloutsOnOneFramePairByKind(t *testing.T) {
	mk := func(kind string) Item {
		return Item{Type: "callout", Part: 1, Session: 1, At: at(160), Fields: map[string]any{
			"rule": "fuel", "kind": kind, "priority": 5.0, "pit_lane": "speak", "slots": nil,
		}}
	}
	r := compareItems(pitRun, []Item{mk("template_pit_next"), mk("template")}, []Item{mk("template"), mk("template_pit_next")})
	if len(r.Diffs) != 0 || r.Callouts.Matched != 2 {
		t.Fatalf("%+v", r.Diffs)
	}
}

// Every event example decodes into the schema; a type the v1 side doesn't give is
// counted, not compared.
func TestEventExamplesNormalise(t *testing.T) {
	sessions := map[string]int{}
	for name, want := range map[string]string{
		"session_started":   `car_code=3431 joined_on_lap=null total_laps=10 at=1200`,
		"session_continued": `at=1`,
		"session_kind":      `cars_on_track=true evidence=6 laps, 2 cars; fuel use on; rolling start at 128 km/h field=true fuel_use=on kind=lap_race start=rolling total_laps=6 at=1384`,
		"session_track":     `confidence=confirmed family= name=Suzuka Circuit East Course track_id=442 at=4790`,
		"pit_entered":       `fuel_before=8.685 in_lap=6 position_before=5 stop=1 at=11245`,
		"pit_exited":        `fuel_added=91.315 fuel_after=100 fuel_before=8.685 in_lap=6 out_lap=7 pit_lane_ms=30220 position_after=10 position_before=5 stationary_ms=17130 stint=2 stop=1 tyres_changed=true at=13056`,
		"pit_teleport":      `entry_at=3 lap=3 at=241`,
		"lap_completed":     `complete=true lap=3 lap_time_ms=98765 pit_role=none at=-`,
		"race_finished":     `kind=flag lap=6 position=1 at=23410`,
		"session_ended":     `finished=false reason=finished at=61840`,
	} {
		typ := strings.Replace(name, "_", ".", 1)
		it, err := v2Item(busMsg{Subject: "twr.v1.parity.evt.S." + typ, Data: example(t, name)}, sessions)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := strings.TrimSpace(fieldsOf(it) + " at=" + it.at()); got != want {
			t.Errorf("%s\n got %s\nwant %s", name, got, want)
		}
	}
	it, err := v2Item(busMsg{Subject: "twr.v1.parity.evt.S.race.position", Data: example(t, "race_position")}, sessions)
	if err != nil || it.Type != "race.position" || len(it.Fields) != 0 {
		t.Fatalf("%v %+v", err, it)
	}
	r := compareItems(pitRun, nil, []Item{{Type: "race.position", Part: 1, Fields: map[string]any{}}})
	if r.Other["race.position"] != 1 || len(r.Diffs) != 0 {
		t.Errorf("%+v", r)
	}
	var out bytes.Buffer
	_ = writeEventResult(&out, r)
	if !strings.Contains(out.String(), "not compared yet (no v1 side): race.position 1") {
		t.Errorf("%s", out.String())
	}
}

func fieldsOf(it Item) string {
	var parts []string
	for k, v := range it.Fields {
		parts = append(parts, k+"="+show(v))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func TestV2ItemsNumberSessionsAndDropRepeats(t *testing.T) {
	started := example(t, "session_started")
	msgs := []busMsg{
		{Subject: "twr.v1.parity.evt.A.session.started", Data: started, ID: "a"},
		{Subject: "twr.v1.parity.evt.A.session.started", Data: started, ID: "a"},
		{Subject: "twr.v1.parity.evt._.telemetry.lost", Data: []byte(`{}`)},
	}
	items, err := v2Items(msgs, 2, map[string]int{}, map[string]bool{})
	if err != nil || len(items) != 2 || items[0].Session != 1 || items[0].Part != 2 || items[1].Type != "telemetry.lost" {
		t.Fatalf("%v %+v", err, items)
	}
	for _, m := range []busMsg{
		{Subject: "twr.v1.parity.frame.gt7", Data: []byte(`{}`)},
		{Subject: "twr.v1.parity.evt.A.session.started", Data: []byte(`{`)},
		{Subject: "twr.v1.parity.callout.p0", Data: []byte(`[]`)},
	} {
		if _, err := v2Items([]busMsg{m}, 1, map[string]int{}, map[string]bool{}); err == nil {
			t.Errorf("%s: no error", m.Subject)
		}
	}
}

func TestEnumAndOpt(t *testing.T) {
	if enum(enumName("FINISH_KIND_UNSPECIFIED"), "FINISH_KIND_") != nil || opt(nil) != nil {
		t.Error("unspecified is null")
	}
}

type enumName string

func (e enumName) String() string { return string(e) }

func item1(typ string, session int, frame uint32, fields map[string]any) Item {
	return Item{Type: typ, Part: 1, Session: session, At: at(frame), Fields: fields}
}

func TestAcceptedDifferences(t *testing.T) {
	soloKind := item1("session.kind", 1, 1, map[string]any{"kind": "solo_laps"})
	raceKind := item1("session.kind", 1, 1, map[string]any{"kind": "lap_race"})
	entry := func(pos any) Item {
		return item1("pit.entered", 1, 9, map[string]any{"stop": 1.0, "in_lap": 1.0, "fuel_before": 1.0, "position_before": pos})
	}
	ended := func(finished bool) Item { return item1("session.ended", 1, 99, map[string]any{"finished": finished}) }
	timed := item1("race.finished", 1, 50, map[string]any{"kind": "timed", "lap": 5.0, "position": 1.0})
	started := func(joined any) Item {
		return item1("session.started", 1, 1, map[string]any{"car_code": 1.0, "total_laps": nil, "joined_on_lap": joined})
	}
	for name, c := range map[string]struct {
		v1, v2 []Item
		want   string // the accepted ID, or "" for a failing difference
	}{
		"solo position":           {[]Item{soloKind, entry(1.0)}, []Item{soloKind, entry(nil)}, "solo-position"},
		"position in a field":     {[]Item{raceKind, entry(1.0)}, []Item{raceKind, entry(nil)}, ""},
		"timed finish cut":        {[]Item{timed, ended(false)}, []Item{timed, ended(true)}, "timed-finish-cut"},
		"finished with no finish": {[]Item{ended(false)}, []Item{ended(true)}, ""},
		"joined":                  {[]Item{started(7.0)}, []Item{started(nil)}, "pending-joined"},
		"start check":             {nil, []Item{started(nil)}, "pending-start-check"},
		"missing on v2":           {[]Item{started(nil)}, nil, ""},
	} {
		r := compareItems(pitRun, c.v1, c.v2)
		if len(r.Diffs) != 1 || r.Diffs[0].Accepted != c.want {
			t.Errorf("%s: %+v", name, r.Diffs)
		}
	}
	for _, a := range accepted {
		if a.ID == "" || !strings.Contains(a.Issue, "#") || a.Reason == "" {
			t.Errorf("an entry without its id, issue or reason: %+v", a)
		}
	}
	for _, n := range notCompared {
		if !strings.Contains(n.Issue, "#") || n.Reason == "" {
			t.Errorf("%+v", n)
		}
	}
}

// v1 applies a carried track and the frame's own on one frame; ecu publishes the
// frame's last. Only the last is compared.
func TestLastTrackOfAFrame(t *testing.T) {
	a := item1("session.track", 1, 46, map[string]any{"track_id": nil, "confidence": "ambiguous", "family": ""})
	b := item1("session.track", 1, 46, map[string]any{"track_id": 100001.0, "confidence": "probable", "family": ""})
	if r := compareItems(pitRun, []Item{a, b}, []Item{b}); len(r.Diffs) != 0 {
		t.Fatalf("%+v", r.Diffs)
	}
}

func TestReadItems(t *testing.T) {
	for _, bad := range []string{`{`, `{"type":"x","part":1}`, `{"fields":{},"part":1}`, `{"type":"x","fields":{}}`} {
		if _, err := readItems([]byte(bad + "\n")); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
	items, err := readItems([]byte("\n" + `{"type":"x","part":1,"session":0,"at":null,"fields":{}}` + "\n"))
	if err != nil || len(items) != 1 || items[0].at() != "-" {
		t.Fatalf("%v %+v", err, items)
	}
}

func TestShowAndSummary(t *testing.T) {
	if show(true) != "true" || show(func() {}) == "" || show(nil) != "null" {
		t.Error("show")
	}
	it := Item{Type: "callout", Fields: map[string]any{"rule": "fuel", "kind": "template", "text": "Box."}}
	if s := summary(it); s != `kind=template "Box."` {
		t.Errorf("%q", s)
	}
	if (Item{Type: "lap.completed", Fields: map[string]any{}}).label() != "lap.completed null" {
		t.Error("label")
	}
}

// v1's side runs v1events.py in v1's image, every recording of the run mounted.
func TestV1Events(t *testing.T) {
	f := &fakeRunner{rules: []rule{{has: "--entrypoint python", out: `{"type":"session.started","part":1,"session":1,"at":42,"fields":{}}` + "\n"}}}
	s := &v1Side{run: f.run, container: "podman", image: "v1:1.0.0"}
	second := Recording{Name: "b.jsonl.gz", Path: "/rec/b.jsonl.gz"}
	items, err := s.Events(context.Background(), Run{Recordings: []Recording{pitRec, second}})
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %+v", err, items)
	}
	call := f.calls[0]
	if !strings.HasPrefix(call, "podman run --rm -v /rec/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz:/recording/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz:ro,z -v /rec/b.jsonl.gz:/recording/b.jsonl.gz:ro,z --entrypoint python v1:1.0.0 -c ") ||
		!strings.HasSuffix(call, " /recording/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz /recording/b.jsonl.gz") ||
		!strings.Contains(call, "def main(") {
		t.Errorf("%s", call)
	}
	f.rules = []rule{{has: "--entrypoint python", err: errors.New("boom")}}
	if _, err := s.Events(context.Background(), pitRun); err == nil {
		t.Error("no error")
	}
	f.rules = []rule{{has: "--entrypoint python", out: "{}\n"}}
	if _, err := s.Events(context.Background(), pitRun); err == nil {
		t.Error("no error for a bad line")
	}
}

// fakeListener hears what the test publishes into it.
type fakeListener struct {
	mu     sync.Mutex
	msgs   []busMsg
	closed bool
}

func (l *fakeListener) Taken() []busMsg {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]busMsg(nil), l.msgs...)
}

func (l *fakeListener) Close() { l.closed = true }

func (l *fakeListener) hear(m busMsg) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, m)
}

// v2's side listens from before the first replay, replays each recording of the
// run in order, and gives each its part; the stack goes with its volumes after.
func TestV2Events(t *testing.T) {
	var n atomic.Int64
	srv := fakeLogbook(&n)
	defer srv.Close()
	f := &fakeRunner{}
	s := newV2(f, srv.URL, false, &bytes.Buffer{})
	l := &fakeListener{}
	var subjects []string
	s.nats = "nats://x"
	s.listen = func(_ string, subj ...string) (listener, error) {
		subjects = subj
		return l, nil
	}
	started := example(t, "session_started")
	replays := 0
	s.run = func(ctx context.Context, argv ...string) ([]byte, error) {
		if strings.Contains(strings.Join(argv, " "), "replay replay") {
			replays++
			if replays == 1 {
				l.hear(busMsg{Subject: "twr.v1.parity.evt.A.session.started", Data: started, ID: "1"})
			} else {
				l.hear(busMsg{Subject: "twr.v1.parity.callout.p2", Data: example(t, "callout_lap_time"), ID: "2"})
			}
		}
		return f.run(ctx, argv...)
	}
	second := Recording{Name: "b.jsonl.gz", Path: "/rec/b.jsonl.gz"}
	items, err := s.Events(context.Background(), Run{Recordings: []Recording{pitRec, second}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Part != 1 || items[1].Part != 2 || items[1].Session != 1 || !l.closed {
		t.Fatalf("%+v", items)
	}
	if strings.Join(subjects, " ") != "twr.v1.parity.evt.> twr.v1.parity.callout.>" {
		t.Errorf("%q", subjects)
	}
	project := "parity-real-nuc-suzuka-pit-stop-2026-10-15-b"
	stack := "-p " + project + " -f /paddock/deploy/compose/compose.yaml "
	if f.called(stack+"up -d") != 1 || f.called(stack+"down --volumes --remove-orphans") != 2 ||
		f.called("/rec/b.jsonl.gz:/recording/b.jsonl.gz:ro,z replay replay --src parity --speed max /recording/b.jsonl.gz") != 1 {
		t.Errorf("%q", f.calls)
	}
}

func TestV2EventsFails(t *testing.T) {
	var n atomic.Int64
	srv := fakeLogbook(&n)
	defer srv.Close()
	boom := errors.New("boom")
	for name, c := range map[string]struct {
		rule   rule
		listen error
		bad    bool
	}{
		"down":   {rule: rule{has: "down", err: boom}},
		"up":     {rule: rule{has: "up -d", err: boom}},
		"replay": {rule: rule{has: "replay replay", err: boom}},
		"listen": {listen: boom},
		"bad":    {bad: true},
	} {
		f := &fakeRunner{rules: []rule{c.rule}}
		s := newV2(f, srv.URL, true, &bytes.Buffer{})
		l := &fakeListener{}
		if c.bad {
			l.hear(busMsg{Subject: "twr.v1.parity.evt.A.session.started", Data: []byte("{")})
		}
		s.listen = func(string, ...string) (listener, error) { return l, c.listen }
		if _, err := s.Events(context.Background(), pitRun); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	s := newV2(&fakeRunner{}, "http://127.0.0.1:1/healthz", false, &bytes.Buffer{})
	if _, err := s.Events(context.Background(), pitRun); err == nil {
		t.Error("no logbook: no error")
	}
	s = newV2(&fakeRunner{}, srv.URL, false, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.waitQuiet(ctx, &fakeListener{}); err == nil {
		t.Error("cancelled: no error")
	}
}

func TestNATSListenWithNoServer(t *testing.T) {
	if _, err := natsListen("nats://127.0.0.1:1", eventSubjects...); err == nil {
		t.Error("no error")
	}
}

func eventSides(v1Items, v2Items []Item) (*cannedSide, *cannedSide) {
	v1, v2 := sidesWith(nil, nil)
	v1.events = map[string][]Item{pitStop(): v1Items}
	v2.events = map[string][]Item{pitStop(): v2Items}
	return v1, v2
}

func runEvents(t *testing.T, v1, v2 Side, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli(context.Background(), append([]string{"events"}, args...), &stdout, &stderr,
		func(options) (Side, Side) { return v1, v2 })
	return code, stdout.String(), stderr.String()
}

func TestTheEventsCommand(t *testing.T) {
	v1, v2 := eventSides(runItems(t, "v1"), runItems(t, "v2"))
	code, out, errOut := runEvents(t, v1, v2, pitStop())
	if code != exitMatch {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	if !regexp.MustCompile(`(?m)^real-nuc-suzuka-pit-stop-2026-10-15\s+15\s+15\s+9\s+15\s+0\s+0\s+0\s+21\s*$`).MatchString(out) ||
		!strings.Contains(out, "parity events: 1 of 1 run match") || !strings.Contains(out, "accepted: pending-callouts") {
		t.Errorf("%s", out)
	}
	bad := runItems(t, "v2")
	bad = bad[1:] // no session.started
	v1, v2 = eventSides(runItems(t, "v1"), bad)
	if code, out, _ := runEvents(t, v1, v2, "--keep", pitStop()); code != exitDiffer || !strings.Contains(out, "normalised items kept in") {
		t.Errorf("exit %d\n%s", code, out)
	} else {
		dir := strings.TrimSpace(out[strings.LastIndex(out, "kept in ")+len("kept in "):])
		defer func() { _ = os.RemoveAll(dir) }()
		if _, err := os.Stat(filepath.Join(dir, "real-nuc-suzuka-pit-stop-2026-10-15.events.v2.jsonl")); err != nil {
			t.Error(err)
		}
	}
}

func TestEventsArguments(t *testing.T) {
	v1, v2 := eventSides(nil, nil)
	if code, _, errOut := runEvents(t, v1, v2, "--all", pitStop()); code != exitRefused || !strings.Contains(errOut, "--all takes no RUN") {
		t.Errorf("%d %s", code, errOut)
	}
	if code, _, _ := runEvents(t, v1, v2, "--bogus"); code != exitRefused {
		t.Error(code)
	}
	if code, _, _ := runEvents(t, v1, v2, "--help"); code != exitMatch {
		t.Error(code)
	}
	if code, _, errOut := runEvents(t, v1, v2, "no-such-recording"); code != exitRefused || errOut == "" {
		t.Error(code)
	}
	if v1.calls != 0 {
		t.Error("a side ran")
	}
	// The defaults and --all: every run is asked for, the restart pair last.
	code, out, _ := runEvents(t, v1, v2)
	if code != exitMatch || strings.Count(out, "\n  match\n") != 3 {
		t.Errorf("%d\n%s", code, out)
	}
	code, out, _ = runEvents(t, v1, v2, "--all")
	if code != exitMatch || strings.Count(out, "\n  match\n") != 26 ||
		!strings.Contains(out, "real-nuc-suzuka-before-restart-2026-10-18.jsonl.gz+real-nuc-suzuka-joined-mid-race-2026-10-18.jsonl.gz\n") {
		t.Errorf("%d\n%s", code, out)
	}
}

func TestEventsSideFailures(t *testing.T) {
	boom := errors.New("boom")
	ok := cannedSide{images: []string{"img"}}
	for name, s := range map[string][2]Side{
		"v1 images": {&failingSide{cannedSide: ok, imagesErr: boom}, &failingSide{cannedSide: ok}},
		"v2 images": {&failingSide{cannedSide: ok}, &failingSide{cannedSide: ok, imagesErr: boom}},
		"v1 events": {&failingSide{cannedSide: ok, eventsErr: boom}, &failingSide{cannedSide: ok}},
		"v2 events": {&failingSide{cannedSide: ok}, &failingSide{cannedSide: ok, eventsErr: boom}},
	} {
		if code, _, errOut := runEvents(t, s[0], s[1], pitStop()); code != exitRefused || !strings.Contains(errOut, "boom") {
			t.Errorf("%s: %d %s", name, code, errOut)
		}
	}
}

func TestAllRuns(t *testing.T) {
	if got := allRuns(map[string]string{"b": "1", "a": "1"}); strings.Join(got, " ") != "a b" {
		t.Errorf("%q", got)
	}
}
