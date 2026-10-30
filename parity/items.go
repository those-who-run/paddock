package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Item is one event or callout, normalised (paddock#104 R1): what ecu or v1
// decided, in v2's names and values, so the two sides compare field by field.
// README.md, "Events and callouts", has the schema.
type Item struct {
	Type string `json:"type"`
	// Part is the recording's place in the run, from 1: frames are counted per
	// recording on both sides.
	Part int `json:"part"`
	// Session is the session's place in the run, from 1; 0 for none open.
	Session int `json:"session"`
	// At is the frame, counted from 1 in its recording; nil for a lap, which is
	// matched by its number.
	At     *uint32        `json:"at"`
	Fields map[string]any `json:"fields"`
}

// compared are the types each side gives and the fields compared for each. A
// field not named here is shown, never compared (text, outcome); accepted.go
// says why each is left out.
var compared = map[string][]string{
	"session.started":   {"car_code", "total_laps", "joined_on_lap"},
	"session.continued": {},
	"session.kind":      {"kind", "total_laps", "field", "fuel_use", "cars_on_track", "start"},
	"session.track":     {"track_id", "confidence", "family"},
	"session.discarded": {"reason", "first_seq"},
	"session.paused":    {"kind", "since_t", "paused_ms", "pause"},
	"session.resumed":   {"kind", "since_t", "pause_ms", "paused_ms", "pause"},
	"pit.entered":       {"stop", "in_lap", "fuel_before", "position_before"},
	"pit.exited": {
		"stop", "stint", "in_lap", "out_lap", "pit_lane_ms", "stationary_ms",
		"fuel_before", "fuel_after", "fuel_added", "tyres_changed", "position_before", "position_after",
	},
	"pit.teleport":   {"lap", "entry_at"},
	"lap.completed":  {"lap", "lap_time_ms", "complete", "pit_role"},
	"race.final_lap": {"lap"},
	"race.position":  {"previous_position", "position"},
	"race.finished":  {"kind", "lap", "position"},
	"session.ended": {
		"started_at", "ended_at", "driving_ms", "paused_ms", "ended_paused", "car_code", "car_name",
		"total_laps", "laps_completed", "best_lap", "best_lap_ms", "fastest_lap_ms", "game_best_lap_ms",
		"total_time_ms", "kind", "finished", "finishing_position", "final_position", "total_cars",
		"joined_on_lap", "stints", "pit_stops", "fuel_burned", "fuel_burned_by_stint", "fuel_stints", "fuel_used",
		"gaps",
	},
	"callout": {"rule", "kind", "priority", "pit_lane", "slots"},
}

// frameTolerance is how many frames apart two items of a type may be and still
// match (R2). Every type starts at zero, as triaged; one grows only with a
// reason written beside it.
var frameTolerance = map[string]uint32{}

// fieldTolerance is how far apart two numbers of a field may be. v1 rounds fuel to
// 0.001 L and pit times to 0.01 s, so the last digit of either can differ.
var fieldTolerance = map[string]float64{
	"fuel_before": 0.001, "fuel_after": 0.001, "fuel_added": 0.001,
	"pit_lane_ms": 10, "stationary_ms": 10,
}

// readItems reads JSON lines, one item each. A line without a type or fields is
// an error, not an item that could match nothing.
func readItems(b []byte) ([]Item, error) {
	var items []Item
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var it Item
		if err := json.Unmarshal(line, &it); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if it.Type == "" || it.Fields == nil || it.Part < 1 {
			return nil, fmt.Errorf("line %d: not an item: %s", n, line)
		}
		items = append(items, it)
	}
	return items, sc.Err()
}

// ident is what identifies an item within its type and part, beside its frame: a
// lap's session and number, a callout's rule.
func (it Item) ident() string {
	switch it.Type {
	case "lap.completed":
		return fmt.Sprintf("%d/%s", it.Session, show(it.Fields["lap"]))
	case "callout":
		return show(it.Fields["rule"])
	}
	return ""
}

func (it Item) at() string {
	if it.At == nil {
		return "-"
	}
	return strconv.FormatUint(uint64(*it.At), 10)
}

// label is the item as a difference names it: the type, and a callout's rule or
// a lap's number.
func (it Item) label() string {
	switch it.Type {
	case "lap.completed":
		return "lap.completed " + show(it.Fields["lap"])
	case "callout":
		return "callout " + show(it.Fields["rule"])
	}
	return it.Type
}

// EventDiff is one way the sides differ. Field is "item" for an item on one side
// only. Accepted names the entry in accepted.go that allows it.
type EventDiff struct {
	Type, Label string
	Part        int
	Session     int
	At          string // v1's frame, then v2's where they differ
	Field       string
	V1, V2      string
	Accepted    string
}

// Pair is two items matched by type and frame (nil on the side without one).
type Pair struct{ V1, V2 *Item }

// matchItems pairs the items of the compared types by type, part, ident and frame,
// within the type's tolerance, in frame order. Items of other types are returned
// apart, as what is not compared yet.
func matchItems(v1, v2 []Item) (pairs []Pair, other map[string]int) {
	type group struct{ v1, v2 []*Item }
	groups := map[string]*group{}
	var keys []string
	other = map[string]int{}
	add := func(items []Item, side int) {
		for i := range items {
			it := &items[i]
			if _, ok := compared[it.Type]; !ok {
				other[it.Type]++
				continue
			}
			k := fmt.Sprintf("%d\x00%s\x00%s", it.Part, it.Type, it.ident())
			g, ok := groups[k]
			if !ok {
				g = &group{}
				groups[k] = g
				keys = append(keys, k)
			}
			if side == 1 {
				g.v1 = append(g.v1, it)
			} else {
				g.v2 = append(g.v2, it)
			}
		}
	}
	add(v1, 1)
	add(v2, 2)
	for _, k := range keys {
		g := groups[k]
		byAt := func(s []*Item) {
			// Within a frame, by kind: two callouts of one rule on one frame (the fuel
			// rule's "box next lap" and its laps left) pair the same whichever a side
			// raised first.
			sort.SliceStable(s, func(i, j int) bool {
				if atOf(s[i]) != atOf(s[j]) {
					return atOf(s[i]) < atOf(s[j])
				}
				return show(s[i].Fields["kind"]) < show(s[j].Fields["kind"])
			})
		}
		byAt(g.v1)
		byAt(g.v2)
		i, j := 0, 0
		for i < len(g.v1) || j < len(g.v2) {
			switch {
			case i == len(g.v1):
				pairs = append(pairs, Pair{V2: g.v2[j]})
				j++
			case j == len(g.v2):
				pairs = append(pairs, Pair{V1: g.v1[i]})
				i++
			default:
				a, b := g.v1[i], g.v2[j]
				tol := int64(frameTolerance[a.Type])
				d := atOf(a) - atOf(b)
				switch {
				case d >= -tol && d <= tol:
					pairs = append(pairs, Pair{V1: a, V2: b})
					i++
					j++
				case d < 0:
					pairs = append(pairs, Pair{V1: a})
					i++
				default:
					pairs = append(pairs, Pair{V2: b})
					j++
				}
			}
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i].first(), pairs[j].first()
		if a.Part != b.Part {
			return a.Part < b.Part
		}
		// Sessions are in frame order, and a lap, which has no frame, then sorts
		// among its own session's items.
		if a.Session != b.Session {
			return a.Session < b.Session
		}
		return atOf(a) < atOf(b)
	})
	return pairs, other
}

func (p Pair) first() *Item {
	if p.V1 != nil {
		return p.V1
	}
	return p.V2
}

// atOf orders an item by its frame; a lap, which has none, by its number, which
// keeps laps in order among themselves.
func atOf(it *Item) int64 {
	if it.At == nil {
		if f, ok := it.Fields["lap"].(float64); ok {
			return int64(f)
		}
		return 0
	}
	return int64(*it.At)
}

// diffPair lists the ways a pair differs: a missing side, the session, the frame
// where a tolerance let it differ, and each compared field.
func diffPair(p Pair) []EventDiff {
	it := p.first()
	base := EventDiff{Type: it.Type, Label: it.label(), Part: it.Part, Session: it.Session, At: it.at()}
	if p.V1 == nil || p.V2 == nil {
		d := base
		d.Field = "item"
		d.V1, d.V2 = "missing", "missing"
		if p.V1 != nil {
			d.V1 = summary(*p.V1)
		} else {
			d.V2 = summary(*p.V2)
		}
		return []EventDiff{d}
	}
	var diffs []EventDiff
	if p.V1.at() != p.V2.at() {
		base.At = p.V1.at() + "/" + p.V2.at()
	}
	if p.V1.Session != p.V2.Session {
		d := base
		d.Field, d.V1, d.V2 = "session", strconv.Itoa(p.V1.Session), strconv.Itoa(p.V2.Session)
		diffs = append(diffs, d)
	}
	for _, f := range compared[it.Type] {
		a, b := p.V1.Fields[f], p.V2.Fields[f]
		if !valueEqual(f, a, b) {
			d := base
			d.Field, d.V1, d.V2 = f, show(a), show(b)
			diffs = append(diffs, d)
		}
	}
	return diffs
}

// valueEqual compares decoded values: numbers within the field's tolerance, the
// rest exactly. A null equals only a null.
func valueEqual(field string, a, b any) bool {
	x, okx := a.(float64)
	y, oky := b.(float64)
	if okx && oky {
		// The epsilon keeps a difference of exactly the tolerance inside it.
		return math.Abs(x-y) <= fieldTolerance[field]+1e-9
	}
	return reflect.DeepEqual(a, b)
}

// show is a decoded value as a difference prints it.
func show(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// summary is an item on one side only, as a difference prints it: its compared
// fields, then its text if it has one, so a reviewer knows which it is.
func summary(it Item) string {
	var parts []string
	for _, f := range compared[it.Type] {
		if v, ok := it.Fields[f]; ok && f != "rule" {
			parts = append(parts, f+"="+show(v))
		}
	}
	if t, ok := it.Fields["text"].(string); ok && t != "" {
		parts = append(parts, strconv.Quote(t))
	}
	return strings.Join(parts, " ")
}
