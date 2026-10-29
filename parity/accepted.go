package main

import "strings"

// The accepted differences between v1 and v2 (paddock#104 R4): this file is the
// one place they are listed, each with its reason and the issue that accepted it,
// and a change to it is reviewed like any other. A difference no entry matches
// fails the run. An entry for something v2 has still to do (pending) is removed
// by the PR that does it, so the diff is strict from then on.

// acceptance is one accepted difference: what it matches and why it is allowed.
type acceptance struct {
	ID     string
	Issue  string
	Reason string
	Match  func(d EventDiff, run runView) bool
}

// runView is what an entry may look at besides the difference: both sides' items,
// and the starts v1 ignored (v1events.py's start.ignored, never compared).
type runView struct{ v1, v2, ignored []Item }

// ignoredAt is whether v1 ignored a start on frame at of part.
func (r runView) ignoredAt(part int, at uint32) bool {
	for _, it := range r.ignored {
		if f, ok := it.Fields["start_at"].(float64); ok && it.Part == part && uint32(f) == at {
			return true
		}
	}
	return false
}

// kindOf is the last kind a side gave a session.
func kindOf(items []Item, part, session int) string {
	kind := ""
	for _, it := range items {
		if it.Type == "session.kind" && it.Part == part && it.Session == session {
			kind = show(it.Fields["kind"])
		}
	}
	return kind
}

func has(items []Item, match func(Item) bool) bool {
	for _, it := range items {
		if match(it) {
			return true
		}
	}
	return false
}

var accepted = []acceptance{
	{
		ID:    "solo-position",
		Issue: "those-who-run/paddock#276",
		Reason: "Outside a field ecu leaves positions out (contracts v0.1.4), where GT7 reports 1 " +
			"and v1 kept it. v1's 1 in a solo_laps or solo_timed session is v2's null.",
		Match: func(d EventDiff, run runView) bool {
			kind := kindOf(run.v1, d.Part, d.Session)
			return strings.HasPrefix(d.Field, "position") && d.V1 == "1" && d.V2 == "null" &&
				(kind == "solo_laps" || kind == "solo_timed")
		},
	},
	{
		ID:     "pending-pit-role",
		Issue:  "those-who-run/paddock#83",
		Reason: "ecu does not set lap.completed's pit role yet (the lap model). Remove with #83.",
		Match: func(d EventDiff, _ runView) bool {
			return d.Type == "lap.completed" && d.Field == "pit_role" && d.V2 == "null"
		},
	},
	{
		ID:    "pending-joined",
		Issue: "those-who-run/paddock#75",
		Reason: "ecu does not mark a session joined part way yet, so session.started has no " +
			"joined_on_lap where v1 stored the lap it joined on. Remove with #75.",
		Match: func(d EventDiff, _ runView) bool {
			return d.Type == "session.started" && d.Field == "joined_on_lap" && d.V2 == "null"
		},
	},
	{
		ID:    "pending-start-check",
		Issue: "those-who-run/paddock#74",
		Reason: "v1 holds a start GT7's cars-on-track flag is off for and drops a replay or a demo " +
			"(v1events.py's start.ignored); ecu has no start check yet and opens the session. A v2 item " +
			"of a session that started on the frame of a start v1 ignored is listed. Remove with #74.",
		Match: func(d EventDiff, run runView) bool {
			if d.Field != "item" || d.V1 != "missing" {
				return false
			}
			for _, it := range run.v2 {
				if it.Type == "session.started" && it.Part == d.Part && it.Session == d.Session && it.At != nil &&
					run.ignoredAt(d.Part, *it.At) {
					return true
				}
			}
			return false
		},
	},
	{
		ID:    "pending-callouts",
		Issue: "those-who-run/paddock#89",
		Reason: "ecu publishes no callouts yet (the rule engine). While a run has none from v2, " +
			"each of v1's is listed; once ecu publishes any, every callout is compared. Remove with #89.",
		Match: func(d EventDiff, run runView) bool {
			return d.Type == "callout" && d.Field == "item" && d.V2 == "missing" &&
				!has(run.v2, func(it Item) bool { return it.Type == "callout" })
		},
	},
}

// notCompared are the fields and values left out of the comparison on purpose,
// with why. They are not differences, so nothing above matches them; they are
// here so every gap between the sides is in one reviewed file.
var notCompared = []struct{ What, Issue, Reason string }{
	{"session.started type", "those-who-run/paddock#82",
		"v1's session type (race, time trial, practice) is replaced in v2 by the session's kind, compared as session.kind."},
	{"session.ended reason", "those-who-run/paddock#86",
		"v1 stored no end reason, so END_REASON_MENU against END_REASON_QUIT and the like has nothing to compare with."},
	{"callout text", "those-who-run/paddock#104",
		"ecu publishes the meaning (rule, kind, slots) and radio phrases it (#71); Brundle's diff compares the spoken text."},
	{"callout slots, while v1's are null", "those-who-run/paddock#91",
		"v1 phrases words, not numbers. A rule's port (#91 to #98) adds its slots to v1events.py; from then on they are compared."},
	{"evidence", "those-who-run/paddock#104",
		"The evidence sentences explain a decision for a person; the decision itself is compared."},
	{"lap fuel, position and frames", "those-who-run/paddock#40",
		"`parity laps` compares the stored laps; lap.completed is here for the lap, its time and its pit role."},
}

// ignoredType is v1events.py's marker for a start v1 held and then ignored: not an
// event of either side, so it is taken out before matching.
const ignoredType = "start.ignored"

// sameFrame are the types v1 may apply more than once on a frame where ecu
// publishes the frame's last decision once: at a start v1 applies the verdict its
// detector carried from the last race, then the frame's own (SUGO's restarts:
// ambiguous, then probable, on frame 46). Only the frame's last is compared.
var sameFrame = map[string]bool{"session.track": true}

// lastPerFrame keeps, of the sameFrame types, v1's last item for each part,
// session and frame. Only v1's: two of ecu's on one frame are a difference.
func lastPerFrame(items []Item) []Item {
	type key struct {
		typ           string
		part, session int
		at            uint32
	}
	last := map[key]int{}
	for i, it := range items {
		if sameFrame[it.Type] && it.At != nil {
			last[key{it.Type, it.Part, it.Session, *it.At}] = i
		}
	}
	var out []Item
	for i, it := range items {
		if sameFrame[it.Type] && it.At != nil && last[key{it.Type, it.Part, it.Session, *it.At}] != i {
			continue
		}
		out = append(out, it)
	}
	return out
}

// accept marks each difference an entry allows with the entry's ID.
func accept(diffs []EventDiff, run runView) {
	for i := range diffs {
		for _, a := range accepted {
			if a.Match(diffs[i], run) {
				diffs[i].Accepted = a.ID
				break
			}
		}
	}
}

// unused are the entries no difference in the results matched. Over every
// recording paddock keeps, an unused entry is stale: what it allowed no longer
// happens, so it goes, and the run fails until it does.
func unused(results []EventResult) []acceptance {
	hits := map[string]int{}
	for _, r := range results {
		for _, d := range r.Diffs {
			hits[d.Accepted]++
		}
	}
	var out []acceptance
	for _, a := range accepted {
		if hits[a.ID] == 0 {
			out = append(out, a)
		}
	}
	return out
}
