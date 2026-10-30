package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	eventsv1 "github.com/those-who-run/contracts/gen/go/twr/events/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// decode reads a message's protobuf JSON (contracts' content type). Fields a later
// contracts adds are ignored, so a newer ecu still decodes.
var decode = protojson.UnmarshalOptions{DiscardUnknown: true}

// v2Items normalises what the checker heard over one part of a run: each event and
// callout, decoded into its contracts message by its subject, in v2's names and
// values as README.md's schema gives them (R1). sessions numbers the run's
// sessions by their ids, in the order they started, across parts. A message sent
// twice (the same Nats-Msg-Id) counts once, as JetStream keeps it once.
func v2Items(msgs []busMsg, part int, sessions map[string]int, seen map[string]bool) ([]Item, error) {
	// Callouts and events are two subjects, so a callout raised on the frame its
	// session starts (start_brief) can arrive before session.started. Number the
	// sessions first, so it still lands in its session.
	for _, m := range msgs {
		if !strings.HasSuffix(m.Subject, ".session.started") {
			continue
		}
		var e eventsv1.SessionStarted
		if err := decode.Unmarshal(m.Data, &e); err != nil {
			return nil, fmt.Errorf("%s: %w", m.Subject, err)
		}
		if _, ok := sessions[e.GetSessionId()]; !ok {
			sessions[e.GetSessionId()] = len(sessions) + 1
		}
	}
	var items []Item
	for _, m := range msgs {
		if m.ID != "" {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
		}
		it, err := v2Item(m, sessions)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.Subject, err)
		}
		it.Part = part
		items = append(items, it)
	}
	return items, nil
}

// v2Item is one message as an item. Its type is the subject's area and what
// (twr.v1.<src>.evt.<sid>.<area>.<what>), or callout (twr.v1.<src>.callout.<level>).
func v2Item(m busMsg, sessions map[string]int) (Item, error) {
	tokens := strings.Split(m.Subject, ".")
	if len(tokens) == 5 && tokens[3] == "callout" {
		var c eventsv1.Callout
		if err := decode.Unmarshal(m.Data, &c); err != nil {
			return Item{}, err
		}
		// No slots and empty slots are one: a phrase with no values (Callout's slots).
		var slots any = map[string]any{}
		if c.Slots != nil {
			b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(c.Slots)
			if err != nil {
				return Item{}, err
			}
			if err := json.Unmarshal(b, &slots); err != nil {
				return Item{}, err
			}
		}
		return item("callout", sessions[c.GetSessionId()], at(c.GetAt()), map[string]any{
			"rule": c.GetRule(), "kind": c.GetKind(), "priority": num(c.GetPriority()),
			"pit_lane": enum(c.GetPitLane(), "PIT_LANE_"), "slots": slots, "text": c.GetText(),
		}), nil
	}
	if len(tokens) != 7 || tokens[3] != "evt" {
		return Item{}, fmt.Errorf("not an event or callout subject")
	}
	typ := tokens[5] + "." + tokens[6]
	msg, fields := v2Message(typ)
	if msg == nil {
		// A type the v1 side doesn't give yet: counted, not compared.
		return Item{Type: typ, Fields: map[string]any{}}, nil
	}
	if err := decode.Unmarshal(m.Data, msg); err != nil {
		return Item{}, err
	}
	id := sessionID(msg)
	if typ == "session.started" {
		if _, ok := sessions[id]; !ok {
			sessions[id] = len(sessions) + 1
		}
	}
	it := fields(msg)
	it.Type = typ
	it.Session = sessions[id]
	return it, nil
}

// sessionID is a message's session_id; every event in v2Message has one.
func sessionID(m proto.Message) string {
	f := m.ProtoReflect().Descriptor().Fields().ByName("session_id")
	if f == nil {
		return ""
	}
	return m.ProtoReflect().Get(f).String()
}

// v2Message is the contracts message for an event type, and how its fields map to
// the schema's; nil for a type the v1 side doesn't give.
func v2Message(typ string) (proto.Message, func(proto.Message) Item) {
	switch typ {
	case "session.started":
		return &eventsv1.SessionStarted{}, func(m proto.Message) Item {
			e := m.(*eventsv1.SessionStarted)
			return item("", 0, at(e.GetFirstSeq()), map[string]any{
				"car_code": num(e.GetCarCode()), "total_laps": opt(e.TotalLaps), "joined_on_lap": opt(e.JoinedOnLap),
			})
		}
	case "session.continued":
		return &eventsv1.SessionContinued{}, func(m proto.Message) Item {
			return item("", 0, at(m.(*eventsv1.SessionContinued).GetAt()), map[string]any{})
		}
	case "session.kind":
		return &eventsv1.SessionKindChanged{}, func(m proto.Message) Item {
			e := m.(*eventsv1.SessionKindChanged)
			k := e.GetKind()
			return item("", 0, at(e.GetAt()), map[string]any{
				"kind": enum(k.GetSessionType(), "SESSION_TYPE_"), "total_laps": opt(k.TotalLaps),
				"field": k.GetField(), "fuel_use": enum(k.GetFuelUse(), "FUEL_USE_"),
				"cars_on_track": k.GetCarsOnTrack(), "start": enum(k.GetStart(), "START_TYPE_"),
				"evidence": k.GetEvidence(),
			})
		}
	case "session.track":
		return &eventsv1.SessionTrackChanged{}, func(m proto.Message) Item {
			e := m.(*eventsv1.SessionTrackChanged)
			t := e.GetTrack()
			return item("", 0, at(e.GetAt()), map[string]any{
				"track_id": opt(t.TrackId), "confidence": enum(t.GetConfidence(), "TRACK_CONFIDENCE_"),
				"family": t.GetFamily(), "name": t.GetName(),
			})
		}
	case "pit.entered":
		return &eventsv1.PitEntered{}, func(m proto.Message) Item {
			e := m.(*eventsv1.PitEntered)
			return item("", 0, at(e.GetAt()), map[string]any{
				"stop": num(e.GetStop()), "in_lap": num(e.GetInLap()), "fuel_before": e.GetFuelBefore(),
				"position_before": opt(e.PositionBefore),
			})
		}
	case "pit.exited":
		return &eventsv1.PitExited{}, func(m proto.Message) Item {
			e := m.(*eventsv1.PitExited)
			s := e.GetStop()
			return item("", 0, at(e.GetAt()), map[string]any{
				"stop": num(s.GetStop()), "stint": num(e.GetStint()), "in_lap": num(s.GetInLap()),
				"out_lap": opt(s.OutLap), "pit_lane_ms": opt(s.PitLaneMs), "stationary_ms": num(s.GetStationaryMs()),
				"fuel_before": s.GetFuelBefore(), "fuel_after": s.GetFuelAfter(), "fuel_added": s.GetFuelAdded(),
				"tyres_changed": s.GetTyresChanged(), "position_before": opt(s.PositionBefore),
				"position_after": opt(s.PositionAfter),
			})
		}
	case "pit.teleport":
		return &eventsv1.PitTeleport{}, func(m proto.Message) Item {
			e := m.(*eventsv1.PitTeleport)
			var entry any
			if e.EntryAt != nil {
				entry = float64(*e.EntryAt)
			}
			return item("", 0, at(e.GetAt()), map[string]any{"lap": num(e.GetLap()), "entry_at": entry})
		}
	case "lap.completed":
		return &eventsv1.LapCompleted{}, func(m proto.Message) Item {
			e := m.(*eventsv1.LapCompleted)
			var lapTime, role any
			if e.GetLapTimeMs() > 0 {
				lapTime = num(e.GetLapTimeMs())
			}
			if e.GetPitRole() != eventsv1.PitRole_PIT_ROLE_UNSPECIFIED {
				role = enum(e.GetPitRole(), "PIT_ROLE_")
			}
			return item("", 0, nil, map[string]any{
				"lap": num(e.GetLap()), "lap_time_ms": lapTime, "complete": e.GetComplete(), "pit_role": role,
			})
		}
	case "race.finished":
		return &eventsv1.RaceFinished{}, func(m proto.Message) Item {
			e := m.(*eventsv1.RaceFinished)
			return item("", 0, at(e.GetAt()), map[string]any{
				"kind": enum(e.GetKind(), "FINISH_KIND_"), "lap": num(e.GetLap()), "position": opt(e.Position),
			})
		}
	case "session.discarded":
		// Not a session's: no session.started came for its id, so it is in session 0,
		// as v1 had none open.
		return &eventsv1.SessionDiscarded{}, func(m proto.Message) Item {
			e := m.(*eventsv1.SessionDiscarded)
			return item("", 0, at(e.GetAt()), map[string]any{
				"reason": enum(e.GetReason(), "DISCARD_REASON_"), "first_seq": float64(e.GetFirstSeq()),
				"evidence": e.GetEvidence(),
			})
		}
	case "session.paused":
		return &eventsv1.SessionPaused{}, func(m proto.Message) Item {
			e := m.(*eventsv1.SessionPaused)
			return item("", 0, at(e.GetAt()), map[string]any{
				"kind": enum(e.GetKind(), "PAUSE_KIND_"), "since_t": e.GetSinceT(), "paused_ms": num(e.GetPausedMs()),
				"pause": num(e.GetPause()),
			})
		}
	case "session.resumed":
		return &eventsv1.SessionResumed{}, func(m proto.Message) Item {
			e := m.(*eventsv1.SessionResumed)
			return item("", 0, at(e.GetAt()), map[string]any{
				"kind": enum(e.GetKind(), "PAUSE_KIND_"), "since_t": e.GetSinceT(), "pause_ms": num(e.GetPauseMs()),
				"paused_ms": num(e.GetPausedMs()), "pause": num(e.GetPause()),
			})
		}
	case "race.final_lap":
		return &eventsv1.RaceFinalLapStarted{}, func(m proto.Message) Item {
			e := m.(*eventsv1.RaceFinalLapStarted)
			return item("", 0, at(e.GetAt()), map[string]any{"lap": num(e.GetLap())})
		}
	case "race.position":
		return &eventsv1.RacePositionChanged{}, func(m proto.Message) Item {
			e := m.(*eventsv1.RacePositionChanged)
			return item("", 0, at(e.GetAt()), map[string]any{
				"previous_position": opt(e.PreviousPosition), "position": num(e.GetPosition()),
			})
		}
	case "session.ended":
		return &eventsv1.SessionEnded{}, func(m proto.Message) Item {
			e := m.(*eventsv1.SessionEnded)
			fields := summaryFields(e.GetSummary())
			fields["reason"] = enum(e.GetReason(), "END_REASON_")
			return item("", 0, at(e.GetLastSeq()), fields)
		}
	}
	return nil, nil
}

// summaryFields are session.ended's summary in the schema's names, as v1events.py
// reads v1's summary.json (paddock#86 AC1): times in epoch milliseconds, an
// optional value null when it is left out, and no car name as null, as v1 had
// none. The laps are `parity laps`'s.
func summaryFields(s *eventsv1.SessionSummary) map[string]any {
	var name any
	if s.GetCarName() != "" {
		name = s.GetCarName()
	}
	floats := func(xs []float64) []any {
		out := []any{}
		for _, x := range xs {
			out = append(out, x)
		}
		return out
	}
	return map[string]any{
		"started_at": millis(s.GetStartedAt()), "ended_at": millis(s.GetEndedAt()),
		"driving_ms": num(s.GetDrivingMs()), "paused_ms": num(s.GetPausedMs()), "ended_paused": s.GetEndedPaused(),
		"car_code": num(s.GetCarCode()), "car_name": name, "total_laps": num(s.GetTotalLaps()),
		"laps_completed": num(s.GetLapsCompleted()), "best_lap": opt(s.BestLap), "best_lap_ms": opt(s.BestLapTimeMs),
		"fastest_lap_ms": opt(s.FastestLapMs), "game_best_lap_ms": opt(s.GameBestLapMs), "total_time_ms": opt(s.TotalTimeMs),
		"kind":     enum(s.GetSessionType(), "SESSION_TYPE_"),
		"finished": s.GetFinished(), "finishing_position": opt(s.FinishingPosition),
		"final_position": opt(s.FinalPosition), "total_cars": opt(s.TotalCars), "joined_on_lap": opt(s.JoinedOnLap),
		"stints": num(s.GetStints()), "pit_stops": float64(len(s.GetPitStops())),
		"fuel_burned": s.GetFuelBurned(), "fuel_burned_by_stint": floats(s.GetFuelBurnedByStint()),
		"fuel_stints": floats(s.GetFuelStints()), "fuel_used": s.GetFuelUsed(),
	}
}

// millis is a time as epoch milliseconds, to the nearest; null when it is not set.
func millis(ts *timestamppb.Timestamp) any {
	if ts == nil {
		return nil
	}
	return math.Round(float64(ts.AsTime().UnixNano()) / 1e6)
}

func item(typ string, session int, at *uint32, fields map[string]any) Item {
	return Item{Type: typ, Session: session, At: at, Fields: fields}
}

func at(n uint32) *uint32 { return &n }

// num is an integer as JSON decodes one, so both sides' values compare alike.
func num(n int32) any { return float64(n) }

// opt is an optional integer: null when it is left out.
func opt(p *int32) any {
	if p == nil {
		return nil
	}
	return float64(*p)
}

// enum is an enum value's name without its prefix, lower case, as v1 names it
// (SESSION_TYPE_LAP_RACE is lap_race); null when unspecified.
func enum(e interface{ String() string }, prefix string) any {
	name := strings.TrimPrefix(e.String(), prefix)
	if name == "UNSPECIFIED" {
		return nil
	}
	return strings.ToLower(name)
}
