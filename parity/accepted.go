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

// runView is what an entry may look at besides the difference: both sides' items.
type runView struct{ v1, v2 []Item }

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
		ID:    "pending-joined",
		Issue: "those-who-run/paddock#75",
		Reason: "ecu does not mark a session joined part way yet, so session.started has no " +
			"joined_on_lap where v1 stored the lap it joined on. Remove with #75.",
		Match: func(d EventDiff, _ runView) bool {
			return d.Type == "session.started" && d.Field == "joined_on_lap" && d.V2 == "null"
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
	{"lap.best", "those-who-run/paddock#103",
		"v1 never decided a best on each change: it stored the session's best at the end, with its own fallback, " +
			"which the lap model's best departs from (#83's decision, R3 over R5). v2's are counted, not compared."},
	{"evidence", "those-who-run/paddock#104",
		"The evidence sentences explain a decision for a person (session.kind's, session.track's, session.discarded's); " +
			"the decision itself is compared."},
	{"lap fuel, position and frames", "those-who-run/paddock#40",
		"`parity laps` compares the stored laps; lap.completed is here for the lap, its time and its pit role."},
}

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
