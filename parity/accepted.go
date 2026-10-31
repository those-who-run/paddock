package main

import (
	"reflect"
	"slices"
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

// trackBy is a side's last session.track of the difference's part and session at
// or before its frame, nil when there is none.
func trackBy(items []Item, d EventDiff) *Item {
	frame, err := strconv.ParseUint(d.At, 10, 32)
	if err != nil {
		return nil
	}
	var last *Item
	for i, it := range items {
		if it.Type == "session.track" && it.Part == d.Part && it.Session == d.Session && it.At != nil && uint64(*it.At) <= frame {
			last = &items[i]
		}
	}
	return last
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

// gridBehindTheLine is where ecu reads a grid start that v1 read as rolling
// (ecu#69, 2026-10-31): the recording, and the frame of its session.kind.
var gridBehindTheLine = map[string]string{
	"real-nuc-sugo-two-stops-2026-10-18": "47",
}

// rankedRuns are the runs with a session ecu reads as ranked and v1 as a timed
// field (ecu#70, ecu 0.1.15, the maintainer's decision of 2026-10-31): qualifying
// on 2026-10-14 (both sessions, the second a restart), and the Spa and Suzuka
// sessions of 2026-10-18, the Suzuka one alone and continued in the restart pair.
// Each names, as "part/frame", where ecu's own items are: the leap's session.kind
// (leaps), its rank calls (ranks: v2's alone, or on v1's frame with another kind)
// and its quit summaries (quits). A ranked item on any other frame is a
// difference: so is one of these no longer raised, as the entry then goes unused.
var rankedRuns = map[string]struct{ leaps, ranks, quits []string }{
	"real-quali-restart-quit-2026-10-14": {
		leaps: []string{"1/31"}, ranks: []string{"1/750"}, quits: []string{"1/774"},
	},
	"real-nuc-spa-pit-cut-quit-2026-10-18": {
		leaps: []string{"1/74"}, ranks: []string{"1/623"}, quits: []string{"1/1192"},
	},
	"real-nuc-suzuka-before-restart-2026-10-18": {
		leaps: []string{"1/49"}, ranks: []string{"1/378", "1/1480"},
	},
	// The continued session's rank is called afresh in the second recording: a
	// restarted ecu calls the first rank it reads as it is.
	"real-nuc-suzuka-before-restart-2026-10-18+real-nuc-suzuka-joined-mid-race-2026-10-18": {
		leaps: []string{"1/49"}, ranks: []string{"1/378", "1/1480", "2/249"},
	},
}

// rankedSession says the difference's session is one v2 reads as ranked and v1
// as a timed field, in one of rankedRuns.
func rankedSession(d EventDiff, run runView) bool {
	_, ok := rankedRuns[run.stem]
	return ok && kindOf(run.v2, d.Part, d.Session) == "ranked" && kindOf(run.v1, d.Part, d.Session) == "timed_field"
}

// pinned says the difference is at one of frames, as "part/frame".
func pinned(d EventDiff, frames []string) bool {
	return slices.Contains(frames, strconv.Itoa(d.Part)+"/"+d.At)
}

// rankDirection says v2's rank kind is v1's position kind as a rank: a place
// gained (template_gained, _lead, _podium) is template_rank_gained, one lost
// (template_lost, _lead) template_rank_lost, and template_rank, the first rank
// called, only with slots of the place alone where v1's had the place before it.
func rankDirection(d EventDiff, run runView) bool {
	switch d.V2 {
	case "template_rank_gained":
		return strings.HasPrefix(d.V1, "template_gained")
	case "template_rank_lost":
		return strings.HasPrefix(d.V1, "template_lost")
	case "template_rank":
		v1, v2 := slotsOf(run.v1, d, "position"), slotsOf(run.v2, d, "position")
		return (strings.HasPrefix(d.V1, "template_gained") || strings.HasPrefix(d.V1, "template_lost")) &&
			v1["previous_position"] != nil && len(v2) == 1 && v2["position"] != nil
	}
	return false
}

// rankedDecision is the maintainer's decision of 2026-10-31 (paddock#146): a
// session ranked by lap time, qualifying or practice with the field out, is its
// own kind (contracts v0.2.7's SESSION_TYPE_RANKED), told from a timed race by
// its place leaping from the grid slot as it opens, and said as one.
const rankedDecision = "The maintainer's decision of 2026-10-31, the ranked kind (ecu#70 and ecu#72, ecu 0.1.15): "

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
			if !summaryConfirmedTrack[run.stem] || d.Label != "callout race_summary" || d.Field != "slots" {
				return false
			}
			slots := slotsOf(run.v2, d, "race_summary")
			if !onlyTrackApart(slots, slotsOf(run.v1, d, "race_summary")) {
				return false
			}
			// The track is the session's, and ecu's verdict on it CONFIRMED.
			track := trackBy(run.v2, d)
			return track != nil && track.Fields["confidence"] == "confirmed" && track.Fields["name"] == slots["track"]
		},
	},
	{
		ID:    "grid-behind-the-line",
		Issue: "those-who-run/paddock#148",
		Reason: "A grid behind the line is a grid start (ecu#69, ecu 0.1.14, 2026-10-31): the car stands on the grid " +
			"on the track screen, then launches and starts lap 1 at speed, so v1's 18-packet start window read it as " +
			"rolling. On sugo-two-stops the car stands in slot 10 for 6 s and starts lap 1 at 113 km/h 4.3 s after " +
			"moving off, as the live grid start of 2026-10-31 does: session.kind's start at frame 47 is v1's rolling " +
			"and v2's grid.",
		Match: func(d EventDiff, run runView) bool {
			frame, ok := gridBehindTheLine[run.stem]
			return ok && d.Type == "session.kind" && d.Field == "start" && d.At == frame &&
				d.V1 == "rolling" && d.V2 == "grid"
		},
	},
	{
		ID:    "ranked-kind",
		Issue: "those-who-run/paddock#146",
		Reason: rankedDecision + "a timed field whose place rises 2 or more above its slot in the start window is " +
			"ranked. On real-quali-restart-quit (slot 20 to P1, and its restart), spa-pit-cut-quit (18 to P1) and " +
			"suzuka-before-restart (16 to P3, alone and continued in the restart pair) v2 publishes a session.kind of " +
			"its own on the leap's frame (31, 74 and 49), and each later session.kind and the summary say ranked " +
			"where v1's say timed_field.",
		Match: func(d EventDiff, run runView) bool {
			if !rankedSession(d, run) || (d.Type != "session.kind" && d.Type != "session.ended") {
				return false
			}
			if d.Field == "kind" {
				return d.V1 == "timed_field" && d.V2 == "ranked"
			}
			return d.Type == "session.kind" && d.Field == "item" && d.V1 == "missing" &&
				strings.HasPrefix(d.V2, "kind=ranked ") && pinned(d, rankedRuns[run.stem].leaps)
		},
	},
	{
		ID:    "ranked-brief",
		Issue: "those-who-run/paddock#146",
		Reason: rankedDecision + "a ranked session's start brief is template_ranked with the field's size alone " +
			"(\"Ranked on lap time, twenty cars on the timesheet.\"), where v1's was the timed brief with the place " +
			"(\"Timed race. P one of twenty.\"): its kind, and its slots v1's total_cars only.",
		Match: func(d EventDiff, run runView) bool {
			if d.Label != "callout start_brief" || !rankedSession(d, run) {
				return false
			}
			switch d.Field {
			case "kind":
				return d.V1 == "template" && d.V2 == "template_ranked"
			case "slots":
				v1, v2 := slotsOf(run.v1, d, "start_brief"), slotsOf(run.v2, d, "start_brief")
				return v1 != nil && v1["timed"] == true &&
					reflect.DeepEqual(v2, map[string]any{"total_cars": v1["total_cars"]})
			}
			return false
		},
	},
	{
		ID:    "ranked-position",
		Issue: "those-who-run/paddock#146",
		Reason: rankedDecision + "a ranked session's rank is called only where the driver's own lap put it, 3 s " +
			"after a crossing that closes a lap counting for best, out of the pit lane: the first as it is " +
			"(template_rank, \"That puts you P four on the timesheet.\"), then template_rank_gained or " +
			"template_rank_lost. v1 called every move as the AI posted times (12 calls in Spa's session, 9 in " +
			"Suzuka's): those are v1's alone. v2's are on the frames rankedRuns pins: alone, or on v1's frame with " +
			"the kind its direction gives (gained to rank_gained, lost to rank_lost, the first rank where v1's had a " +
			"place before it) and, for the first, slots of the place alone at v1's place.",
		Match: func(d EventDiff, run runView) bool {
			if d.Label != "callout position" || !rankedSession(d, run) {
				return false
			}
			ranks := rankedRuns[run.stem].ranks
			switch d.Field {
			case "item":
				if d.V2 == "missing" {
					return true
				}
				return d.V1 == "missing" && strings.HasPrefix(d.V2, "kind=template_rank") && pinned(d, ranks)
			case "kind":
				return pinned(d, ranks) && rankDirection(d, run)
			case "slots":
				v1, v2 := slotsOf(run.v1, d, "position"), slotsOf(run.v2, d, "position")
				return pinned(d, ranks) && v1 != nil && v1["previous_position"] != nil &&
					reflect.DeepEqual(v2, map[string]any{"position": v1["position"]})
			}
			return false
		},
	},
	{
		ID:    "ranked-quit",
		Issue: "those-who-run/paddock#146",
		Reason: rankedDecision + "ranked is in race_summary's quit_kinds, so leaving a ranked session after a lap " +
			"says the laps and the best lap (template_quit), where v1, reading a timed field, said nothing: on Spa " +
			"(frame 1192) and the qualifying's second session (774).",
		Match: func(d EventDiff, run runView) bool {
			return d.Label == "callout race_summary" && d.Field == "item" && d.V1 == "missing" &&
				strings.HasPrefix(d.V2, "kind=template_quit ") && rankedSession(d, run) &&
				pinned(d, rankedRuns[run.stem].quits)
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
