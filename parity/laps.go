package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
)

// Lap is one stored lap, normalised (R2): what the two sides are compared on.
// Session is the session's place in the run (1, 2, …), since each side mints its
// own session ids. A null is a nil pointer, written as null.
type Lap struct {
	Session   int      `json:"session"`
	Lap       int      `json:"lap"`
	LapTimeMs *int     `json:"lap_time_ms"`
	Complete  bool     `json:"complete"`
	FuelStart *float64 `json:"fuel_start"`
	FuelEnd   *float64 `json:"fuel_end"`
	FuelUsed  *float64 `json:"fuel_used"`
	Position  *int     `json:"position"`
	// Frames is compared only under --frames, and never fails a run (R7).
	Frames int `json:"frames"`
}

// storedLap is a lap as a side's store gives it: logbook export --laps's JSON
// lines on v2, and the same keys from v1's index.sqlite (v1.go). Keys not named
// here are ignored.
type storedLap struct {
	SessionID string   `json:"session_id"`
	Lap       int      `json:"lap"`
	LapTimeMs *int     `json:"lap_time_ms"`
	Complete  bool     `json:"complete"`
	FuelStart *float64 `json:"fuel_start"`
	FuelEnd   *float64 `json:"fuel_end"`
	FuelUsed  *float64 `json:"fuel_used"`
	Position  *int     `json:"position"`
	Frames    int      `json:"frames"`
	// SessionKind is v1's sessions.session_kind; v2's export has no session columns.
	SessionKind string `json:"session_kind"`
}

// soloKinds are v1's session kinds with no field. GT7 reports position 1 there and
// v1 stored it; since contracts v0.1.4 ecu leaves the position out (paddock#276),
// so v1's 1 in these sessions is compared as absent.
var soloKinds = map[string]bool{"solo_laps": true, "solo_timed": true}

// readStoredLaps reads JSON lines, one lap each, in the store's order: by session
// start, then lap.
func readStoredLaps(b []byte) ([]storedLap, error) {
	var laps []storedLap
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var l storedLap
		if err := json.Unmarshal(line, &l); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if l.SessionID == "" {
			return nil, fmt.Errorf("line %d: no session_id", n)
		}
		laps = append(laps, l)
	}
	return laps, sc.Err()
}

// normalise numbers the sessions in the order they first appear, which is their
// start order on both sides, and maps v1's solo position (soloKinds).
func normalise(stored []storedLap) []Lap {
	sessions := map[string]int{}
	laps := make([]Lap, 0, len(stored))
	for _, s := range stored {
		n, ok := sessions[s.SessionID]
		if !ok {
			n = len(sessions) + 1
			sessions[s.SessionID] = n
		}
		position := s.Position
		if soloKinds[s.SessionKind] && position != nil && *position == 1 {
			position = nil
		}
		laps = append(laps, Lap{
			Session: n, Lap: s.Lap, LapTimeMs: s.LapTimeMs, Complete: s.Complete,
			FuelStart: s.FuelStart, FuelEnd: s.FuelEnd, FuelUsed: s.FuelUsed,
			Position: position, Frames: s.Frames,
		})
	}
	return laps
}

// sessionCount is the number of sessions the laps belong to.
func sessionCount(laps []Lap) int {
	n := 0
	for _, l := range laps {
		n = max(n, l.Session)
	}
	return n
}
