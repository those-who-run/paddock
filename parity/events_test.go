package main

import (
	"bytes"
	"compress/gzip"
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
	"time"

	eventsv1 "github.com/those-who-run/contracts/gen/go/twr/events/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// testdata/run holds one run of the pit stop recording as each side gave it, made
// with v1events.py on v1 1.0.0 and with ecu 0.1.10's race machine and rules (its
// rulestest.Replay, every output encoded as ecu's publisher encodes it and read by
// v2Items): the real items, normalised, so the tests compare what the sides give.
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

// The pit stop through v1 and through ecu 0.1.10: every event and callout matches,
// slots, pit roles, the final lap, the positions, the pause and the summary included, with
// nothing accepted. Only the bests are left, which v1 has no side for.
func TestTheRealPitStopRunMatches(t *testing.T) {
	r := compareItems(pitRun, runItems(t, "v1"), runItems(t, "v2"))
	if len(r.Diffs) > 0 {
		t.Fatalf("%+v", r.Diffs)
	}
	if r.Events.V1 != 20 || r.Events.V2 != 20 || r.Events.Matched != 20 ||
		r.Callouts.V1 != 15 || r.Callouts.V2 != 15 || r.Callouts.Matched != 15 {
		t.Errorf("events %+v callouts %+v", r.Events, r.Callouts)
	}
	if len(r.Other) != 1 || r.Other["lap.best"] != 2 {
		t.Errorf("not compared %v", r.Other)
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

// Every rule's slots are compared: a v1 side without them is a difference, and a
// callout with no slots on the bus is the same as one with empty slots.
func TestSlotsAreAlwaysCompared(t *testing.T) {
	v2 := lapTimeCallout(t)
	v1 := cloneItem(v2)
	v1.Fields["slots"] = nil
	if f := failing(compareItems(pitRun, []Item{v1}, []Item{v2})); len(f) != 1 || f[0].Field != "slots" {
		t.Fatalf("%+v", f)
	}
	final := []byte(`{"sessionId":"S","rule":"final_lap","kind":"template","priority":10,"at":7,"pitLane":"PIT_LANE_HOLD"}`)
	it, err := v2Item(busMsg{Subject: "twr.v1.parity.callout.p1", Data: final}, map[string]int{"S": 1})
	if err != nil || show(it.Fields["slots"]) != "{}" {
		t.Fatalf("%v %+v", err, it)
	}
}

// A callout on one side only fails, whatever the other side published.
func TestAMissingCalloutFails(t *testing.T) {
	v1 := runItems(t, "v1")
	r := compareItems(pitRun, v1, append(runItems(t, "v2"), lapTimeCallout(t)))
	if f := failing(r); len(f) != 1 || f[0].V1 != "missing" || f[0].Label != "callout lap_time" {
		t.Fatalf("%+v", f)
	}
	var v2 []Item
	for _, it := range runItems(t, "v2") {
		if it.Type != "callout" || show(it.Fields["rule"]) != "position" {
			v2 = append(v2, it)
		}
	}
	if f := failing(compareItems(pitRun, v1, v2)); len(f) != 2 || f[0].V2 != "missing" {
		t.Fatalf("%+v", f)
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
		"session_ended": `best_lap=4 best_lap_ms=97412 car_code=0 car_name=null driving_ms=0 ended_at=null ended_paused=false ` +
			`fastest_lap_ms=null final_position=1 finished=false finishing_position=1 fuel_burned=0 fuel_burned_by_stint=[] ` +
			`fuel_stints=[] fuel_used=37.625 game_best_lap_ms=null gaps=[] joined_on_lap=null kind=lap_race laps_completed=10 paused_ms=0 ` +
			`pit_stops=0 reason=finished started_at=null stints=0 total_cars=null total_laps=0 total_time_ms=null at=61840`,
		"race_position":     `position=3 previous_position=11 at=31877`,
		"race_final_lap":    `lap=6 at=19237`,
		"session_discarded": `evidence=fuel is draining (98.3 -> 97.6), which only a replay does first_seq=40210 reason=replay at=40291`,
		"session_paused":    `kind=frozen pause=2 paused_ms=1200 since_t=2412.375 at=9921`,
		"session_resumed":   `kind=frozen pause=2 pause_ms=45000 paused_ms=46200 since_t=2412.375 at=12601`,
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
	it, err := v2Item(busMsg{Subject: "twr.v1.parity.evt.S.lap.best", Data: example(t, "lap_best")}, sessions)
	if err != nil || it.Type != "lap.best" || len(it.Fields) != 0 {
		t.Fatalf("%v %+v", err, it)
	}
	r := compareItems(pitRun, nil, []Item{{Type: "lap.best", Part: 1, Fields: map[string]any{}}})
	if r.Other["lap.best"] != 1 || len(r.Diffs) != 0 {
		t.Errorf("%+v", r)
	}
	var out bytes.Buffer
	_ = writeEventResult(&out, r)
	if !strings.Contains(out.String(), "not compared yet (no v1 side): lap.best 1") {
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

// A callout heard before its session's session.started still lands in that
// session: the two come on different subjects, in either order.
func TestV2ItemsACalloutBeforeItsStart(t *testing.T) {
	msgs := []busMsg{
		{Subject: "twr.v1.parity.callout.p2", Data: example(t, "callout_lap_time")},
		{Subject: "twr.v1.parity.evt.A.session.started", Data: example(t, "session_started")},
	}
	items, err := v2Items(msgs, 1, map[string]int{}, map[string]bool{})
	if err != nil || len(items) != 2 || items[0].Type != "callout" || items[0].Session != 1 || items[1].Session != 1 {
		t.Fatalf("%v %+v", err, items)
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
	started := func(joined any) Item {
		return item1("session.started", 1, 1, map[string]any{"car_code": 1.0, "total_laps": nil, "joined_on_lap": joined})
	}
	// A held start on v1's side names the later frame it decided on.
	held := item1("session.started", 1, 1, map[string]any{"car_code": 1.0, "total_laps": nil, "joined_on_lap": nil, "decided_at": 4.0})
	summary := func(field string, v any) Item { return item1("session.ended", 1, 99, map[string]any{field: v}) }
	// A best lap's summary on each side: one field, beside the GT7 best both sides agree on.
	best := func(field string, v, game any) Item {
		return item1("session.ended", 1, 99, map[string]any{field: v, "game_best_lap_ms": game})
	}
	lap := func(n float64, ms any, complete bool) Item {
		return Item{Type: "lap.completed", Part: 1, Session: 1, Fields: map[string]any{"lap": n, "lap_time_ms": ms, "complete": complete, "pit_role": "none"}}
	}
	lapOne := []Item{lap(1, 504963.0, true), lap(2, nil, false)}
	twoTimed := []Item{lap(1, 504963.0, true), lap(2, 500204.0, true)}
	for name, c := range map[string]struct {
		v1, v2 []Item
		want   string // the accepted ID, or "" for a failing difference
	}{
		"solo position":           {[]Item{soloKind, entry(1.0)}, []Item{soloKind, entry(nil)}, "solo-position"},
		"position in a field":     {[]Item{raceKind, entry(1.0)}, []Item{raceKind, entry(nil)}, ""},
		"finished with no finish": {[]Item{ended(false)}, []Item{ended(true)}, ""},
		"no joined_on_lap on v2":  {[]Item{started(7.0)}, []Item{started(nil)}, ""},
		"no start on v1":          {nil, []Item{started(nil)}, ""},
		"missing on v2":           {[]Item{started(nil)}, nil, ""},
		// paddock#86 AC1: ecu#52's three departures, each as narrow as it is.
		"solo final position": {
			[]Item{soloKind, summary("final_position", 1.0)}, []Item{soloKind, summary("final_position", nil)}, "solo-final-position",
		},
		"final position in a field": {
			[]Item{raceKind, summary("final_position", 1.0)}, []Item{raceKind, summary("final_position", nil)}, "",
		},
		"solo final position not 1": {
			[]Item{soloKind, summary("final_position", 2.0)}, []Item{soloKind, summary("final_position", nil)}, "",
		},
		"best lap 1, the only timed": {
			append([]Item{best("best_lap_ms", 504963.0, nil)}, lapOne...), append([]Item{best("best_lap_ms", nil, nil)}, lapOne...), "best-lap-one",
		},
		"best lap number 1": {
			append([]Item{best("best_lap", 1.0, nil)}, lapOne...), append([]Item{best("best_lap", nil, nil)}, lapOne...), "best-lap-one",
		},
		"best lap with another timed": {
			append([]Item{best("best_lap_ms", 504963.0, nil)}, twoTimed...), append([]Item{best("best_lap_ms", nil, nil)}, twoTimed...), "",
		},
		"best lap not lap 1's time": {
			append([]Item{best("best_lap_ms", 500000.0, nil)}, lapOne...), append([]Item{best("best_lap_ms", nil, nil)}, lapOne...), "",
		},
		"best lap 1 with a GT7 best": {
			append([]Item{best("best_lap_ms", 504963.0, 504963.0)}, lapOne...), append([]Item{best("best_lap_ms", nil, 504963.0)}, lapOne...), "",
		},
		"best lap number 1 with a GT7 best": {
			append([]Item{best("best_lap", 1.0, 504963.0)}, lapOne...), append([]Item{best("best_lap", nil, 504963.0)}, lapOne...), "",
		},
		"best lap on v2 only differs": {
			append([]Item{best("best_lap_ms", 504963.0, nil)}, lapOne...), append([]Item{best("best_lap_ms", 500000.0, nil)}, lapOne...), "",
		},
		"a held start 3 s later": {
			[]Item{held, summary("started_at", 1767225603001.0)}, []Item{held, summary("started_at", 1767225600000.0)}, "held-start-started-at",
		},
		"a held start too late": {
			[]Item{held, summary("started_at", 1767225605001.0)}, []Item{held, summary("started_at", 1767225600000.0)}, "",
		},
		"a held start earlier on v1": {
			[]Item{held, summary("started_at", 1767225599000.0)}, []Item{held, summary("started_at", 1767225600000.0)}, "",
		},
		"started_at with no hold": {
			[]Item{started(nil), summary("started_at", 1767225603001.0)}, []Item{started(nil), summary("started_at", 1767225600000.0)}, "",
		},
		"ended_at of a held start": {
			[]Item{held, summary("ended_at", 1767225603001.0)}, []Item{held, summary("ended_at", 1767225600000.0)}, "",
		},
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

// The decision of 2026-10-31, no track at the start: each entry allows only the
// track, only on its callout, and only on the recordings it lists.
func TestNoTrackAtTheStart(t *testing.T) {
	run := func(stem string) Run { return Run{Recordings: []Recording{{Name: stem + ".jsonl.gz"}}} }
	callout := func(rule string, slots map[string]any) Item {
		return item1("callout", 1, 48, map[string]any{"rule": rule, "kind": "template", "priority": 8.0, "pit_lane": "hold", "slots": slots})
	}
	brief := func(track any) Item {
		slots := map[string]any{"laps": 12.0, "position": 10.0, "total_cars": 20.0}
		if track != nil {
			slots["track"] = track
		}
		return callout("start_brief", slots)
	}
	summary := func(track any) Item {
		slots := map[string]any{"best_lap_ms": 82190.0, "position": 9.0}
		if track != nil {
			slots["track"] = track
		}
		return callout("race_summary", slots)
	}
	trackOnly := callout("start_brief", map[string]any{"track": "Fuji International Speedway"})
	// ecu's verdict on the session's track, before the summary's frame (48).
	verdict := func(confidence, name string) Item {
		return item1("session.track", 1, 40, map[string]any{"track_id": 1.0, "confidence": confidence, "family": "sugo", "name": name})
	}
	confirmed := verdict("confirmed", "Sportsland SUGO")
	const (
		fuji = "real-nuc-fuji-timed-2026-10-15"
		sugo = "real-nuc-sugo-two-stops-2026-10-18"
		spa  = "real-nuc-spa-pit-cut-quit-2026-10-18"
	)
	for name, c := range map[string]struct {
		stem   string
		v1, v2 []Item
		want   string
	}{
		"a track-only brief":                      {fuji, []Item{trackOnly}, nil, "no-track-brief"},
		"a track-only brief elsewhere":            {spa, []Item{trackOnly}, nil, ""},
		"a brief with more, missing":              {fuji, []Item{brief("Fuji International Speedway")}, nil, ""},
		"a track-only brief only on v2":           {fuji, nil, []Item{trackOnly}, ""},
		"the brief's track":                       {sugo, []Item{brief("Sportsland SUGO")}, []Item{brief(nil)}, "no-track-brief-slot"},
		"the brief's track elsewhere":             {spa, []Item{brief("Sportsland SUGO")}, []Item{brief(nil)}, ""},
		"the brief's track on v2":                 {sugo, []Item{brief(nil)}, []Item{brief("Sportsland SUGO")}, ""},
		"the brief's track and more":              {sugo, []Item{brief("Sportsland SUGO")}, []Item{callout("start_brief", map[string]any{"laps": 12.0})}, ""},
		"the summary's track":                     {sugo, []Item{summary(nil)}, []Item{confirmed, summary("Sportsland SUGO")}, "summary-confirmed-track"},
		"the summary's track, probable":           {sugo, []Item{summary(nil)}, []Item{verdict("probable", "Sportsland SUGO"), summary("Sportsland SUGO")}, ""},
		"the summary's track, another":            {sugo, []Item{summary(nil)}, []Item{verdict("confirmed", "Fuji International Speedway"), summary("Sportsland SUGO")}, ""},
		"the summary's track, no verdict":         {sugo, []Item{summary(nil)}, []Item{summary("Sportsland SUGO")}, ""},
		"the summary's track, a verdict after it": {sugo, []Item{summary(nil)}, []Item{item1("session.track", 1, 60, confirmed.Fields), summary("Sportsland SUGO")}, ""},
		"the summary's track elsewhere":           {spa, []Item{summary(nil)}, []Item{confirmed, summary("Sportsland SUGO")}, ""},
		"the summary's track on v1":               {sugo, []Item{summary("Sportsland SUGO")}, []Item{confirmed, summary(nil)}, ""},
		"the summary's track, empty":              {sugo, []Item{summary(nil)}, []Item{confirmed, summary("")}, ""},
		"the summary's track and more":            {sugo, []Item{callout("race_summary", map[string]any{"position": 9.0})}, []Item{confirmed, summary("Sportsland SUGO")}, ""},
	} {
		// v1 gives the same verdict (its name isn't compared), so the callout is the one difference.
		v1 := c.v1
		for _, it := range c.v2 {
			if it.Type == "session.track" {
				v1 = append([]Item{it}, v1...)
			}
		}
		r := compareItems(run(c.stem), v1, c.v2)
		if len(r.Diffs) != 1 || r.Diffs[0].Accepted != c.want {
			t.Errorf("%s: %+v", name, r.Diffs)
		}
	}
}

// paddock#148: on sugo-two-stops, session.kind's start at frame 47 is v1's rolling
// and v2's grid (ecu#69). Nothing else is: another recording, another frame,
// another field, the other way round or another start.
func TestAGridBehindTheLine(t *testing.T) {
	run := func(stem string) Run { return Run{Recordings: []Recording{{Name: stem + ".jsonl.gz"}}} }
	kind := func(frame uint32, start string) Item {
		return item1("session.kind", 1, frame, map[string]any{"kind": "lap_race", "total_laps": 12.0, "field": true,
			"fuel_use": "on", "cars_on_track": true, "start": start})
	}
	const (
		sugo   = "real-nuc-sugo-two-stops-2026-10-18"
		suzuka = "real-nuc-suzuka-no-stop-2026-10-15"
	)
	withLaps := func(laps float64) Item {
		k := kind(47, "grid")
		k.Fields["total_laps"] = laps
		return k
	}
	for name, c := range map[string]struct {
		stem   string
		v1, v2 Item
		want   string
	}{
		"rolling to grid":       {sugo, kind(47, "rolling"), kind(47, "grid"), "grid-behind-the-line"},
		"another recording":     {suzuka, kind(47, "rolling"), kind(47, "grid"), ""},
		"another frame":         {sugo, kind(48, "rolling"), kind(48, "grid"), ""},
		"grid to rolling":       {sugo, kind(47, "grid"), kind(47, "rolling"), ""},
		"rolling to flying":     {sugo, kind(47, "rolling"), kind(47, "flying"), ""},
		"unknown to grid":       {sugo, kind(47, "unknown"), kind(47, "grid"), ""},
		"another field differs": {sugo, kind(47, "grid"), withLaps(5), ""},
	} {
		r := compareItems(run(c.stem), []Item{c.v1}, []Item{c.v2})
		if len(r.Diffs) != 1 || r.Diffs[0].Accepted != c.want {
			t.Errorf("%s: %+v", name, r.Diffs)
		}
	}
}

// A start v1 held and discarded is compared with ecu's session.discarded: its
// reason and first frame. A session ecu opened where v1 discarded the start, or the
// other way round, is a difference; so is a continued session against a new one.
func TestADiscardedStartIsCompared(t *testing.T) {
	discarded := func(reason string, first float64) Item {
		return item1("session.discarded", 0, 30, map[string]any{"reason": reason, "first_seq": first, "evidence": "x"})
	}
	if r := compareItems(pitRun, []Item{discarded("demo", 18)}, []Item{discarded("demo", 18)}); len(r.Diffs) != 0 {
		t.Fatalf("%+v", r.Diffs)
	}
	if f := failing(compareItems(pitRun, []Item{discarded("demo", 18)}, []Item{discarded("replay", 18)})); len(f) != 1 || f[0].Field != "reason" {
		t.Fatalf("%+v", f)
	}
	if f := failing(compareItems(pitRun, []Item{discarded("demo", 18)}, []Item{discarded("demo", 19)})); len(f) != 1 || f[0].Field != "first_seq" {
		t.Fatalf("%+v", f)
	}
	started := item1("session.started", 1, 30, map[string]any{"car_code": 1.0, "total_laps": 12.0, "joined_on_lap": nil})
	if f := failing(compareItems(pitRun, []Item{discarded("replay", 18)}, []Item{started})); len(f) != 2 {
		t.Fatalf("a session where v1 discarded the start: %+v", f)
	}
	continued := Item{Type: "session.continued", Part: 2, Session: 1, At: at(1), Fields: map[string]any{}}
	second := Item{Type: "session.started", Part: 2, Session: 2, At: at(1), Fields: started.Fields}
	if f := failing(compareItems(pitRun, []Item{started, continued}, []Item{started, second})); len(f) != 2 {
		t.Fatalf("the restart pair's part 2: %+v", f)
	}
}

// v1 applies a carried track and the frame's own on one frame; ecu publishes the
// frame's last. Only the last is compared.

// Two of ecu's tracks on one frame are a difference: only v1's are taken as the
// frame's last.
func TestADuplicateV2TrackIsADifference(t *testing.T) {
	b := item1("session.track", 1, 46, map[string]any{"track_id": 100001.0, "confidence": "probable", "family": ""})
	if f := failing(compareItems(pitRun, []Item{b}, []Item{b, b})); len(f) != 1 || f[0].V1 != "missing" {
		t.Fatalf("%+v", f)
	}
}

// An accepted entry that allows nothing is noted; over every recording it fails
// the run.
func TestUnusedEntries(t *testing.T) {
	v1, v2 := eventSides(runItems(t, "v1"), runItems(t, "v2"))
	var out bytes.Buffer
	code := compareEvents(context.Background(), []Run{pitRun}, v1, v2, options{}, &out, &bytes.Buffer{})
	if code != exitMatch || !strings.Contains(out.String(), "accepted.go: solo-position (those-who-run/paddock#276) allowed no difference in these runs") {
		t.Fatalf("%d\n%s", code, out.String())
	}
	out.Reset()
	code = compareEvents(context.Background(), []Run{pitRun}, v1, v2, options{everyRecording: true}, &out, &bytes.Buffer{})
	if code != exitDiffer || !strings.Contains(out.String(), "8 accepted entries unused over every recording: remove them") {
		t.Fatalf("%d\n%s", code, out.String())
	}
	sums := map[string]string{pitRec.Name: "x"}
	if !everyRecording([]Run{pitRun}, sums) || everyRecording([]Run{pitRun}, map[string]string{"other": "x"}) ||
		everyRecording(nil, nil) {
		t.Error("everyRecording")
	}
}

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
		!strings.HasSuffix(call, " --gap 8.7 /recording/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz /recording/b.jsonl.gz") ||
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
	// The first recording's frames are read for the second's --clock.
	first := pitRec
	first.Path = filepath.Join(t.TempDir(), first.Name)
	writeGzip(t, first.Path, `{"t":100.25}`+"\n"+`{"t":160.75}`+"\n")
	second := Recording{Name: "b.jsonl.gz", Path: "/rec/b.jsonl.gz"}
	items, err := s.Events(context.Background(), Run{Recordings: []Recording{first, second}})
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
		f.called(":ro,z replay replay --src parity --speed max --keep-open /recording/"+pitRec.Name) != 1 ||
		f.called("/rec/b.jsonl.gz:/recording/b.jsonl.gz:ro,z replay replay --src parity --speed max "+
			"--clock 2026-01-01T00:01:09.2Z /recording/b.jsonl.gz") != 1 {
		t.Errorf("%q", f.calls)
	}
	// A recording whose frames can't be read replays nothing.
	f.calls = nil
	missing := Recording{Name: "gone.jsonl.gz", Path: filepath.Join(t.TempDir(), "gone.jsonl.gz")}
	if _, err := s.Events(context.Background(), Run{Recordings: []Recording{missing, second}}); err == nil || len(f.calls) != 0 {
		t.Errorf("%v %q", err, f.calls)
	}
}

func writeGzip(t *testing.T, path, text string) {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	_, _ = gz.Write([]byte(text))
	_ = gz.Close()
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The restart pair, as ecu#56 gives the commands: one --src, the first replay
// kept open, the second's --clock its last frame plus 8.7 s, from the recording.
func TestTheRestartPairsReplays(t *testing.T) {
	recs, err := resolveRecordings(restartPair, filepath.Join("..", "recordings"), readRepoSources(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := replayArgs(Run{Recordings: recs})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"replay replay --src parity --speed max --keep-open /recording/real-nuc-suzuka-before-restart-2026-10-18.jsonl.gz",
		"replay replay --src parity --speed max --clock 2026-01-01T00:24:01.476810287Z /recording/real-nuc-suzuka-joined-mid-race-2026-10-18.jsonl.gz",
	}
	if len(got) != 2 || strings.Join(got[0], " ") != want[0] || strings.Join(got[1], " ") != want[1] {
		t.Errorf("%q", got)
	}
	// A recording on its own: no --clock and nothing kept, as before.
	got, err = replayArgs(pitRun)
	if err != nil || len(got) != 1 || strings.Join(got[0], " ") != "replay replay --src parity --speed max /recording/"+pitRec.Name {
		t.Errorf("%v %q", err, got)
	}
}

// A continued session's gaps are compared, in v1's names and values.
func TestTheSummarysGaps(t *testing.T) {
	g := &eventsv1.SessionGap{
		StartedAt: timestamppb.New(time.Date(2026, 1, 1, 0, 23, 52, 777e6, time.UTC)),
		EndedAt:   timestamppb.New(time.Date(2026, 1, 1, 0, 24, 1, 477e6, time.UTC)),
		Lap:       7, Reason: eventsv1.GapReason_GAP_REASON_ENGINEER_RESTART,
	}
	got := summaryFields(&eventsv1.SessionSummary{Gaps: []*eventsv1.SessionGap{g}})["gaps"]
	if s := show(got); s != `[{"ended_at":1767227041477,"lap":7,"reason":"engineer_restart","started_at":1767227032777}]` {
		t.Errorf("%s", s)
	}
	v1 := []any{map[string]any{"started_at": 1767227032777.0, "ended_at": 1767227041477.0, "lap": 7.0, "reason": "engineer_restart"}}
	if !valueEqual("gaps", v1, got) {
		t.Error("v1's gap differs")
	}
	if show(summaryFields(&eventsv1.SessionSummary{})["gaps"]) != "[]" {
		t.Error("no gaps")
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
	if !regexp.MustCompile(`(?m)^real-nuc-suzuka-pit-stop-2026-10-15\s+20\s+20\s+20\s+15\s+15\s+15\s+0\s+0\s*$`).MatchString(out) ||
		!strings.Contains(out, "parity events: 1 of 1 run match") || !strings.Contains(out, "not compared yet (no v1 side): lap.best 2") {
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
	// Every recording with no differences at all: every accepted entry is unused,
	// which over every recording fails.
	code, out, _ = runEvents(t, v1, v2, "--all")
	if code != exitDiffer || strings.Count(out, "\n  match\n") != 26 || !strings.Contains(out, "unused over every recording") ||
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
