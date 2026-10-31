package main

// The known differences in what is spoken (paddock#64 R3): the one reviewed list of
// the places radio may say something other than v1 on the same recording, each
// with the decision behind it and the issue that took it. A difference no entry
// matches fails `parity spoken`, and so does an entry whose recording was compared
// and that matched nothing: once v2 says what v1 said, the entry goes.
//
// A real difference in spoken text is a bug in radio or ecu, to be fixed there, not
// listed here. Only a difference a decision already accepts elsewhere belongs here.

// knownDifference is one: on Recording (its stem), a callout of Rule that v1 said
// as V1 and radio as V2, "" for the side that said nothing in its place.
type knownDifference struct {
	ID        string
	Issue     string
	Decision  string
	Recording string
	Rule      string
	V1, V2    string
}

// noTrackAtTheStart's decision (accepted.go) as spoken, on paddock#64's recordings:
// the start brief names no track, and the race summary names a track ecu confirmed.
const (
	noTrackBrief   = "The maintainer's decision of 2026-10-31, no track at the start: the start brief names no track (ecu#60)."
	confirmedTrack = "The maintainer's decision of 2026-10-31, no track at the start: the race summary names the track " +
		"when ecu's verdict is CONFIRMED (ecu#60), in radio's v2 phrase (radio#31)."
)

var knownSpoken = []knownDifference{
	{
		ID: "no-track-brief-sugo-two-stops", Issue: "those-who-run/ecu#60", Decision: noTrackBrief,
		Recording: "real-nuc-sugo-two-stops-2026-10-18", Rule: "start_brief",
		V1: "Sportsland SUGO. Twelve laps. P ten of twenty.", V2: "Twelve laps. P ten of twenty.",
	},
	{
		ID: "no-track-brief-fuji-timed", Issue: "those-who-run/ecu#60", Decision: noTrackBrief,
		Recording: "real-nuc-fuji-timed-2026-10-15", Rule: "start_brief",
		V1: "Fuji International Speedway.", V2: "",
	},
	{
		ID: "summary-track-sugo-two-stops", Issue: "those-who-run/radio#31", Decision: confirmedTrack,
		Recording: "real-nuc-sugo-two-stops-2026-10-18", Rule: "race_summary",
		V1: "Chequered flag. Finished ninth. Best lap one twenty-two point two.",
		V2: "Chequered flag at Sportsland SUGO. Finished ninth. Best lap one twenty-two point two.",
	},
	{
		ID: "summary-track-suzuka-no-stop", Issue: "those-who-run/radio#31", Decision: confirmedTrack,
		Recording: "real-nuc-suzuka-no-stop-2026-10-15", Rule: "race_summary",
		V1: "Chequered flag. Finished second. Best lap one oh one point two.",
		V2: "Chequered flag at Suzuka Circuit East Course. Finished second. Best lap one oh one point two.",
	},
	{
		ID: "summary-track-suzuka-pit-stop", Issue: "those-who-run/radio#31", Decision: confirmedTrack,
		Recording: "real-nuc-suzuka-pit-stop-2026-10-15", Rule: "race_summary",
		V1: "Chequered flag. Finished first. Best lap fifty-nine point five.",
		V2: "Chequered flag at Suzuka Circuit East Course. Finished first. Best lap fifty-nine point five.",
	},
	{
		ID: "summary-track-suzuka-timed-race", Issue: "those-who-run/radio#31", Decision: confirmedTrack,
		Recording: "real-nuc-suzuka-timed-race-2026-10-16", Rule: "race_summary",
		V1: "Chequered flag. Finished first. Best lap one oh seven point six.",
		V2: "Chequered flag at Suzuka Circuit East Course. Finished first. Best lap one oh seven point six.",
	},
}
