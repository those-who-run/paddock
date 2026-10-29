package main

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
)

// fuelTolerance is R2's: fuel is compared to 0.001 L. v1 stores its floats as
// read and logbook stores ecu's, so the last digits can differ on the same lap.
const fuelTolerance = 0.001

// Difference is one way a lap differs between the sides. Field is "lap" when the
// lap is on one side only.
type Difference struct {
	Session, Lap int
	Field        string
	V1, V2       string
	// Note marks a difference that is reported and doesn't fail the run (--frames).
	Note bool
}

// Result is one recording's comparison.
type Result struct {
	Recording Recording
	V1, V2    []Lap
	Diffs     []Difference
}

// Match is whether the sides agree on every lap, notes aside.
func (r Result) Match() bool {
	for _, d := range r.Diffs {
		if !d.Note {
			return false
		}
	}
	return true
}

type lapKey struct{ session, lap int }

// compare pairs the laps by session place and lap number, and lists every
// difference, in v1's order and then the laps only v2 has. With frames, the frame
// count per lap is compared too, as notes.
func compare(v1, v2 []Lap, frames bool) []Difference {
	byKey := map[lapKey]Lap{}
	for _, l := range v2 {
		byKey[lapKey{l.Session, l.Lap}] = l
	}
	var diffs []Difference
	seen := map[lapKey]bool{}
	for _, a := range v1 {
		k := lapKey{a.Session, a.Lap}
		seen[k] = true
		b, ok := byKey[k]
		if !ok {
			diffs = append(diffs, Difference{Session: a.Session, Lap: a.Lap, Field: "lap", V1: "stored", V2: "missing"})
			continue
		}
		add := func(field, x, y string, note bool) {
			if x != y {
				diffs = append(diffs, Difference{Session: a.Session, Lap: a.Lap, Field: field, V1: x, V2: y, Note: note})
			}
		}
		add("lap_time_ms", showInt(a.LapTimeMs), showInt(b.LapTimeMs), false)
		add("complete", strconv.FormatBool(a.Complete), strconv.FormatBool(b.Complete), false)
		for _, f := range []struct {
			name string
			x, y *float64
		}{{"fuel_start", a.FuelStart, b.FuelStart}, {"fuel_end", a.FuelEnd, b.FuelEnd}, {"fuel_used", a.FuelUsed, b.FuelUsed}} {
			if !fuelEqual(f.x, f.y) {
				diffs = append(diffs, Difference{Session: a.Session, Lap: a.Lap, Field: f.name, V1: showFuel(f.x), V2: showFuel(f.y)})
			}
		}
		add("position", showInt(a.Position), showInt(b.Position), false)
		if frames {
			add("frames", strconv.Itoa(a.Frames), strconv.Itoa(b.Frames), true)
		}
	}
	for _, b := range v2 {
		if k := (lapKey{b.Session, b.Lap}); !seen[k] {
			diffs = append(diffs, Difference{Session: b.Session, Lap: b.Lap, Field: "lap", V1: "missing", V2: "stored"})
		}
	}
	return diffs
}

func fuelEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	// The epsilon keeps a difference of exactly 0.001 inside the tolerance despite
	// binary rounding.
	return math.Abs(*a-*b) <= fuelTolerance+1e-9
}

func showInt(p *int) string {
	if p == nil {
		return "null"
	}
	return strconv.Itoa(*p)
}

func showFuel(p *float64) string {
	if p == nil {
		return "null"
	}
	return strconv.FormatFloat(*p, 'f', 3, 64)
}

// writeResult prints one recording's table (R3): its sessions and laps on each
// side, then each difference. It is built whole and written once, so a failing
// writer is one error.
func writeResult(w io.Writer, r Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s  sha256 %s\n", r.Recording.Name, r.Recording.SHA256)
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "\t\tv1\tv2\n")
	_, _ = fmt.Fprintf(tw, "\tsessions\t%d\t%d\n", sessionCount(r.V1), sessionCount(r.V2))
	_, _ = fmt.Fprintf(tw, "\tlaps\t%d\t%d\n", len(r.V1), len(r.V2))
	_ = tw.Flush() // a strings.Builder doesn't fail
	failing := 0
	if len(r.Diffs) > 0 {
		tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintf(tw, "\tsession\tlap\tfield\tv1\tv2\t\n")
		for _, d := range r.Diffs {
			note := ""
			if d.Note {
				note = "(note)"
			} else {
				failing++
			}
			_, _ = fmt.Fprintf(tw, "\t%d\t%d\t%s\t%s\t%s\t%s\n", d.Session, d.Lap, d.Field, d.V1, d.V2, note)
		}
		_ = tw.Flush()
	}
	if failing > 0 {
		fmt.Fprintf(&b, "  differ: %d %s\n", failing, plural(failing, "difference", "differences"))
	} else {
		b.WriteString("  match\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
