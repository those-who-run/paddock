package main

import (
	"reflect"
	"strconv"
	"strings"
)

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
// and the run's stem (Run.Stem) for an entry only some recordings may use.
type runView struct {
	v1, v2 []Item
	stem   string
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

// onlyLapOne is the time of a session's lap 1 when, on that side, it is the
// session's only complete lap with a time; 0 otherwise.
func onlyLapOne(items []Item, part, session int) float64 {
	var ms float64
	n := 0
	for _, it := range items {
		if it.Type != "lap.completed" || it.Part != part || it.Session != session || it.Fields["complete"] != true {
			continue
		}
		t, ok := it.Fields["lap_time_ms"].(float64)
		if !ok {
			continue
		}
		n++
		if it.Fields["lap"] == 1.0 {
			ms = t
		}
	}
	if n != 1 {
		return 0
	}
	return ms
}

// noGameBest says whether a side's summary of a session has no GT7 best: with
// one, lap 1 counts for best when GT7 set it there (LapCompleted.counts_for_best).
func noGameBest(items []Item, part, session int) bool {
	for _, it := range items {
		if it.Type == "session.ended" && it.Part == part && it.Session == session {
			v, ok := it.Fields["game_best_lap_ms"]
			return ok && v == nil
		}
	}
	return false
}

// heldStart says whether a side started a session from a held start: v1's
// session.started names the later frame it decided on (v1events.py).
func heldStart(items []Item, part, session int) bool {
	for _, it := range items {
		if it.Type == "session.started" && it.Part == part && it.Session == session {
			return it.Fields["decided_at"] != nil
		}
	}
	return false
}

// slotsOf is a callout's slots on a side: the one of rule at the difference's
// part, session and frame, nil when there is none.
func slotsOf(items []Item, d EventDiff, rule string) map[string]any {
	for _, it := range items {
		if it.Type == "callout" && show(it.Fields["rule"]) == rule && it.Part == d.Part && it.Session == d.Session && it.at() == d.At {
			slots, _ := it.Fields["slots"].(map[string]any)
			return slots
		}
	}
	return nil
}

// onlyTrackApart says whether with names a track and without is with less that
// track, every other slot the same.
func onlyTrackApart(with, without map[string]any) bool {
	track, ok := with["track"].(string)
	if !ok || track == "" || without == nil || without["track"] != nil {
		return false
	}
	rest := map[string]any{}
	for k, v := range with {
		if k != "track" {
			rest[k] = v
		}
	}
	return reflect.DeepEqual(rest, without)
}

// noTrackAtTheStart is the maintainer's decision of 2026-10-31, in ecu 0.1.12
// (ecu#60) and radio 0.1.5 (radio#31): the start brief names no track, and the
// race summary names the track only when ecu's verdict is CONFIRMED. No paddock
// issue holds it. Each entry below lists the recordings it was seen on.
const noTrackAtTheStart = "The maintainer's decision of 2026-10-31, no track at the start: "

// startBriefTrackOnly are the recordings where v1 raised a start_brief that
// named only the track: a time trial's or practice's brief (template), or the
// track on its own after the brief (template_track, real-quali-restart-quit's two).
var startBriefTrackOnly = map[string]bool{
	"real-nuc-fuji-timed-2026-10-15":     true,
	"real-nuc-spa-time-trial-2026-10-18": true,
	"real-nuc-sugo-timed-2026-10-15":     true,
	"real-nuc-unknown-track-2026-10-15":  true,
	"real-quali-restart-quit-2026-10-14": true,
}

// startBriefTrack are the recordings whose race start_brief v1 gave a track slot.
var startBriefTrack = map[string]bool{
	"real-n24h-race-lap1-positions-2026-10-14":   true,
	"real-nuc-gilles-villeneuve-race-2026-10-17": true,
	"real-nuc-sugo-restarts-2026-10-18":          true,
	"real-nuc-sugo-two-stops-2026-10-18":         true,
}

// summaryConfirmedTrack are the recordings whose race_summary names a track
// ecu confirmed: Sportsland SUGO and Suzuka Circuit East Course.
var summaryConfirmedTrack = map[string]bool{
	"real-nuc-sugo-restarts-2026-10-18":     true,
	"real-nuc-sugo-timed-2026-10-15":        true,
	"real-nuc-sugo-two-stops-2026-10-18":    true,
	"real-nuc-suzuka-no-stop-2026-10-15":    true,
	"real-nuc-suzuka-pit-stop-2026-10-15":   true,
	"real-nuc-suzuka-timed-race-2026-10-16": true,
}

// maxHeldMs bounds how much later v1 dated a held start than ecu: the start
// check's hold, 3 s in the one recording with one (real-race-2laps-2026-10-14),
// as ecu's summary test bounds it (maxHeldSecs).
const maxHeldMs = 5000

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
		ID:    "solo-final-position",
		Issue: "those-who-run/paddock#86",
		Reason: "The summary's side of solo-position (#276, ecu#52's first departure): outside a field " +
			"SessionSummary has no final_position (contracts v0.1.4), where v1's summary.json kept GT7's 1. " +
			"v1's 1 in a solo_laps or solo_timed session is v2's null.",
		Match: func(d EventDiff, run runView) bool {
			kind := kindOf(run.v1, d.Part, d.Session)
			return d.Type == "session.ended" && d.Field == "final_position" && d.V1 == "1" && d.V2 == "null" &&
				(kind == "solo_laps" || kind == "solo_timed")
		},
	},
	{
		ID:    "best-lap-one",
		Issue: "those-who-run/paddock#83",
		Reason: "With no GT7 best (v1's gt7_best_lap_ms null), the summary's best lap is the quickest lap that counts (contracts v0.2.3's " +
			"SessionSummary.best_lap, #83's R3 over R5; ecu#52's second departure), and lap 1 does not count, where " +
			"v1's fallback took lap 1 when it was the only timed lap. On the 24 h race and the first qualifying of " +
			"2026-10-14, v1's best is that lap 1 and v2 has none.",
		Match: func(d EventDiff, run runView) bool {
			if d.Type != "session.ended" || d.V2 != "null" || !noGameBest(run.v1, d.Part, d.Session) {
				return false
			}
			ms := onlyLapOne(run.v1, d.Part, d.Session)
			if ms == 0 {
				return false
			}
			return (d.Field == "best_lap" && d.V1 == "1") || (d.Field == "best_lap_ms" && d.V1 == show(ms))
		},
	},
	{
		ID:    "held-start-started-at",
		Issue: "those-who-run/paddock#74",
		Reason: "A held start's session starts at its first held frame (contracts v0.2.3's SessionStarted.started_at; " +
			"ecu#52's third departure), where v1's clock read the frame the start check gave its verdict on. " +
			"v1's started_at is up to the hold later, 3 s on real-race-2laps-2026-10-14's practice.",
		Match: func(d EventDiff, run runView) bool {
			if d.Type != "session.ended" || d.Field != "started_at" || !heldStart(run.v1, d.Part, d.Session) {
				return false
			}
			v1, err1 := strconv.ParseFloat(d.V1, 64)
			v2, err2 := strconv.ParseFloat(d.V2, 64)
			return err1 == nil && err2 == nil && v1 > v2 && v1-v2 <= maxHeldMs
		},
	},
	{
		ID:    "no-track-brief",
		Issue: "those-who-run/ecu#60",
		Reason: noTrackAtTheStart + "a start_brief that only named the track is no longer raised. On fuji-timed, " +
			"spa-time-trial, sugo-timed and unknown-track v1's time trial or practice brief was the track alone " +
			"(\"Fuji International Speedway.\"); on real-quali-restart-quit its two template_track callouts. v2 has none.",
		Match: func(d EventDiff, run runView) bool {
			if !startBriefTrackOnly[run.stem] || d.Label != "callout start_brief" || d.Field != "item" || d.V2 != "missing" {
				return false
			}
			slots := slotsOf(run.v1, d, "start_brief")
			return len(slots) == 1 && onlyTrackApart(slots, map[string]any{})
		},
	},
	{
		ID:    "no-track-brief-slot",
		Issue: "those-who-run/ecu#60",
		Reason: noTrackAtTheStart + "a race's start_brief has no track slot. On gilles-villeneuve-race, " +
			"sugo-two-stops, sugo-restarts (sessions 1 to 3) and n24h-race-lap1-positions v1's slots name the " +
			"track and v2's are the same without it.",
		Match: func(d EventDiff, run runView) bool {
			return startBriefTrack[run.stem] && d.Label == "callout start_brief" && d.Field == "slots" &&
				onlyTrackApart(slotsOf(run.v1, d, "start_brief"), slotsOf(run.v2, d, "start_brief"))
		},
	},
	{
		ID:    "summary-confirmed-track",
		Issue: "those-who-run/ecu#60",
		Reason: noTrackAtTheStart + "the race_summary names the track when ecu's verdict is CONFIRMED (radio#31 " +
			"says it, \"Chequered flag at Sportsland SUGO.\"), where v1's never did. On sugo-restarts (session 3), " +
			"sugo-timed, sugo-two-stops, suzuka-no-stop, suzuka-pit-stop and suzuka-timed-race v2's slots are v1's " +
			"and the track.",
		Match: func(d EventDiff, run runView) bool {
			return summaryConfirmedTrack[run.stem] && d.Label == "callout race_summary" && d.Field == "slots" &&
				onlyTrackApart(slotsOf(run.v2, d, "race_summary"), slotsOf(run.v1, d, "race_summary"))
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
	{"session.ended laps", "those-who-run/paddock#86",
		"The summary's laps[] are the stored laps, which `parity laps` compares lap by lap."},
	{"session.ended track", "those-who-run/paddock#86",
		"The summary's track is session.track's last verdict, compared on each session.track."},
	{"session.ended fuel_capacity, fuel_at_start and fuel_at_end", "those-who-run/paddock#86",
		"Raw fuel levels of a frame, which v1's rounded recordings hold only at float32 precision; `parity laps` " +
			"compares each lap's fuel at its start and end, and session.ended compares fuel_used, fuel_burned and the stints."},
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
