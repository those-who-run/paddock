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
// difference, in v1's order and then the laps only v2 has. A lap stored on one side
// only, or more than once on either, is a difference of its own, never overwritten.
// With frames, the frame count per lap is compared too, as notes.
func compare(v1, v2 []Lap, frames bool) []Difference {
	type pair struct{ v1, v2 []Lap }
	pairs := map[lapKey]*pair{}
	var order []lapKey
	get := func(l Lap) *pair {
		k := lapKey{l.Session, l.Lap}
		p, ok := pairs[k]
		if !ok {
			p = &pair{}
			pairs[k] = p
			order = append(order, k)
		}
		return p
	}
	for _, l := range v1 {
		p := get(l)
		p.v1 = append(p.v1, l)
	}
	for _, l := range v2 {
		p := get(l)
		p.v2 = append(p.v2, l)
	}
	var diffs []Difference
	for _, k := range order {
		p := pairs[k]
		if len(p.v1) != 1 || len(p.v2) != 1 {
			diffs = append(diffs, Difference{Session: k.session, Lap: k.lap, Field: "lap", V1: stored(len(p.v1)), V2: stored(len(p.v2))})
			continue
		}
		a, b := p.v1[0], p.v2[0]
		add := func(field, x, y string, note bool) {
			if x != y {
				diffs = append(diffs, Difference{Session: k.session, Lap: k.lap, Field: field, V1: x, V2: y, Note: note})
			}
		}
		add("lap_time_ms", showInt(a.LapTimeMs), showInt(b.LapTimeMs), false)
		add("complete", strconv.FormatBool(a.Complete), strconv.FormatBool(b.Complete), false)
		for _, f := range []struct {
			name string
			x, y *float64
		}{{"fuel_start", a.FuelStart, b.FuelStart}, {"fuel_end", a.FuelEnd, b.FuelEnd}, {"fuel_used", a.FuelUsed, b.FuelUsed}} {
			if !fuelEqual(f.x, f.y) {
				diffs = append(diffs, Difference{Session: k.session, Lap: k.lap, Field: f.name, V1: showFuel(f.x), V2: showFuel(f.y)})
			}
		}
		add("position", showInt(a.Position), showInt(b.Position), false)
		if frames {
			add("frames", strconv.Itoa(a.Frames), strconv.Itoa(b.Frames), true)
		}
	}
	return diffs
}

// stored says how many times a side stored a lap.
func stored(n int) string {
	switch n {
	case 0:
		return "missing"
	case 1:
		return "stored"
	default:
		return fmt.Sprintf("stored %d times", n)
	}
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
	// Four decimals, one more than the tolerance, so a failing difference never
	// prints as one inside it (95.0000 against 95.0011, not 95.000 against 95.001).
	return strconv.FormatFloat(*p, 'f', 4, 64)
}

// writeResult prints one recording's table (R3): its sessions and laps on each
// side, then each difference. It is built whole and written once, so a failing
// writer is one error.
func writeResult(w io.Writer, r Result) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s  sha256 %s\n", r.Recording.Name, r.Recording.SHA256)
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "\t\tv1\tv2\n")
	_, _ = fmt.Fprintf(tw, "\tsessions with laps\t%d\t%d\n", sessionCount(r.V1), sessionCount(r.V2))
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
