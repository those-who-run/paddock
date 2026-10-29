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
		ID:    "timed-finish-cut",
		Issue: "those-who-run/ecu#32",
		Reason: "A session cut after its timed finish (the recording ends during GT7's coast) is " +
			"finished in v2: the finish was published. v1 marked it finished only at the end it saw.",
		Match: func(d EventDiff, run runView) bool {
			return d.Type == "session.ended" && d.Field == "finished" && d.V1 == "false" && d.V2 == "true" &&
				has(run.v2, func(it Item) bool {
					return it.Type == "race.finished" && it.Part == d.Part && it.Session == d.Session &&
						it.Fields["kind"] == "timed"
				})
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
		Reason: "v1 holds a start GT7's cars-on-track flag is off for and drops a replay, a demo or " +
			"a race GT7 never flags; ecu has no start check yet and opens the session. In a part where " +
			"v1 opened no session, v2's items are listed. Remove with #74.",
		Match: func(d EventDiff, run runView) bool {
			return d.Field == "item" && d.V1 == "missing" &&
				!has(run.v1, func(it Item) bool { return it.Part == d.Part && it.Type == "session.started" })
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

// sameFrame are the types v1 may apply more than once on a frame where ecu
// publishes the frame's last decision once: at a start v1 applies the verdict its
// detector carried from the last race, then the frame's own (SUGO's restarts:
// ambiguous, then probable, on frame 46). Only the frame's last is compared.
var sameFrame = map[string]bool{"session.track": true}

// lastPerFrame keeps, of the sameFrame types, a side's last item for each part,
// session and frame.
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
