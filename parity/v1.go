package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// defaultV1Image is v1 1.0.0, by tag and by the digest of its index (R5).
const defaultV1Image = "docker.io/muldoon/those-who-run:1.0.0@sha256:80fc9b779e73724aced63d72cdd69b94811d5b41232857182744260e1db7665a"

// v1Clock dates v1's stored sessions. A recording's own times are monotonic, not
// wall clock, so "the recording's first frame" has no date of its own; this is the
// time ecu replay gives a recording's first frame when it is passed no --clock
// (ecu's README, "Replay"), so both stores carry the same dates. Dates aren't
// compared (R2): it is there for a person comparing by eye.
const v1Clock = "2026-01-01T00:00:00Z"

// v1Dump prints v1's stored laps, with their session's kind, as JSON lines in the
// order logbook export uses: by session start, then lap. It runs in v1's own image,
// whose Python has sqlite3, so the harness needs no SQLite driver.
const v1Dump = `import json, sqlite3, sys
db = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
keys = ("session_id", "session_kind", "lap", "lap_time_ms", "complete",
        "fuel_start", "fuel_end", "fuel_used", "position", "frames")
for row in db.execute("""SELECT l.session_id, s.session_kind, l.lap, l.lap_time_ms,
        l.complete, l.fuel_start, l.fuel_end, l.fuel_used, l.position, l.frames
        FROM laps l JOIN sessions s ON s.id = l.session_id
        ORDER BY s.started_at, s.id, l.lap"""):
    lap = dict(zip(keys, row))
    lap["complete"] = bool(lap["complete"])
    print(json.dumps(lap))
`

// v1Side replays through v1's image: `replay RECORDING --store /data --clock T`
// into a volume of its own, then reads index.sqlite's sessions and laps (R1).
type v1Side struct {
	run       runner
	container string // docker, or podman
	image     string
	keep      bool
	log       io.Writer
}

func (s *v1Side) Images(context.Context) ([]string, error) {
	return []string{s.image}, nil
}

func (s *v1Side) Laps(ctx context.Context, rec Recording, _ int) (laps []Lap, err error) {
	volume := "parity-v1-" + safeName(rec.Stem())
	// A volume left by an earlier --keep would add its sessions to this run's.
	if _, err := s.run(ctx, s.container, "volume", "rm", "--force", volume); err != nil {
		return nil, err
	}
	if _, err := s.run(ctx, s.container, "volume", "create", volume); err != nil {
		return nil, err
	}
	defer func() {
		if s.keep {
			_, _ = fmt.Fprintf(s.log, "parity: kept v1's store, volume %s\n", volume)
			return
		}
		if _, rmErr := s.run(ctx, s.container, "volume", "rm", "--force", volume); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	// Mounted at /data, which the image makes and gives to its user (1000), so a
	// new volume there takes that owner and v1 can write to it. `z` relabels the
	// recording where SELinux is on (podman on Fedora); docker elsewhere ignores it.
	inside := "/recording/" + rec.Name
	if _, err := s.run(ctx, s.container, "run", "--rm",
		"-v", volume+":/data",
		"-v", rec.Path+":"+inside+":ro,z",
		s.image, "replay", inside, "--store", "/data", "--clock", v1Clock); err != nil {
		return nil, fmt.Errorf("v1 replay: %w", err)
	}
	out, err := s.run(ctx, s.container, "run", "--rm", "-v", volume+":/data",
		"--entrypoint", "python", s.image, "-c", v1Dump, "/data/index.sqlite")
	if err != nil {
		return nil, fmt.Errorf("v1 store: %w", err)
	}
	stored, err := readStoredLaps(out)
	if err != nil {
		return nil, fmt.Errorf("v1 store: %w", err)
	}
	return normalise(stored), nil
}

var unsafeName = regexp.MustCompile(`[^a-z0-9-]+`)

// safeName is a recording's stem as a volume or compose project name: lower case,
// with anything outside [a-z0-9-] as -.
func safeName(stem string) string {
	return strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(stem), "-"), "-")
}

// v1Events is v1events.py: v1's engine over a run, its decisions as items.
//
//go:embed v1events.py
var v1Events string

// Events replays the run's recordings through v1's engine in v1's own image and
// reads the items v1events.py writes (paddock#104). It needs no volume: the store
// is a temporary one inside the container.
func (s *v1Side) Events(ctx context.Context, run Run) ([]Item, error) {
	args := []string{s.container, "run", "--rm"}
	var inside []string
	for _, rec := range run.Recordings {
		path := "/recording/" + rec.Name
		args = append(args, "-v", rec.Path+":"+path+":ro,z")
		inside = append(inside, path)
	}
	args = append(args, "--entrypoint", "python", s.image, "-c", v1Events)
	out, err := s.run(ctx, append(args, inside...)...)
	if err != nil {
		return nil, fmt.Errorf("v1 events: %w", err)
	}
	items, err := readItems(out)
	if err != nil {
		return nil, fmt.Errorf("v1 events: %w", err)
	}
	return items, nil
}
