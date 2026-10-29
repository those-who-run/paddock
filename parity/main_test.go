package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// cannedSide is a side with laps already stored: no containers.
type cannedSide struct {
	images []string
	laps   map[string][]Lap // by recording name
	calls  int
}

func (c *cannedSide) Images(context.Context) ([]string, error) {
	c.calls++
	return c.images, nil
}

func (c *cannedSide) Laps(_ context.Context, rec Recording, _ int) ([]Lap, error) {
	c.calls++
	return c.laps[rec.Name], nil
}

func ip(n int) *int          { return &n }
func fp(f float64) *float64  { return &f }
func clone(laps []Lap) []Lap { return append([]Lap(nil), laps...) }
func pitStop() string        { return "real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz" }

// raceLaps are v1's stored laps of the pit stop recording, from a v1 1.0.0 replay
// (laps 1 to 4; lap 4 is the stop, where the fuel goes up).
var raceLaps = []Lap{
	{Session: 1, Lap: 1, LapTimeMs: ip(57109), Complete: true, FuelStart: fp(96.7237), FuelEnd: fp(65.5994), FuelUsed: fp(31.124), Position: ip(1), Frames: 118},
	{Session: 1, Lap: 2, LapTimeMs: ip(60538), Complete: true, FuelStart: fp(65.5994), FuelEnd: fp(37.9084), FuelUsed: fp(27.691), Position: ip(1), Frames: 121},
	{Session: 1, Lap: 3, LapTimeMs: ip(82544), Complete: true, FuelStart: fp(37.9084), FuelEnd: fp(22.8989), FuelUsed: fp(15.009), Position: ip(1), Frames: 166},
	{Session: 1, Lap: 4, LapTimeMs: ip(69892), Complete: true, FuelStart: fp(22.8989), FuelEnd: fp(81.4760), FuelUsed: fp(-58.577), Position: ip(1), Frames: 146},
}

func runCLI(t *testing.T, v1, v2 Side, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli(context.Background(), append([]string{"laps"}, args...), &stdout, &stderr,
		func(options) (Side, Side) { return v1, v2 })
	return code, stdout.String(), stderr.String()
}

func sidesWith(v1Laps, v2Laps []Lap) (*cannedSide, *cannedSide) {
	v1 := &cannedSide{images: []string{"docker.io/muldoon/those-who-run:1.0.0"}, laps: map[string][]Lap{pitStop(): v1Laps}}
	v2 := &cannedSide{
		images: []string{"ghcr.io/those-who-run/twr-ecu:0.1.4", "ghcr.io/those-who-run/twr-logbook:0.1.4"},
		laps:   map[string][]Lap{pitStop(): v2Laps},
	}
	return v1, v2
}

func TestTheSameLapsMatch(t *testing.T) {
	v1, v2 := sidesWith(raceLaps, clone(raceLaps))
	code, out, errOut := runCLI(t, v1, v2, pitStop())
	if code != exitMatch {
		t.Fatalf("exit %d, want 0\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "parity laps: 1 of 1 recording match") || !strings.Contains(out, "  match\n") {
		t.Errorf("output:\n%s", out)
	}
}

// AC2: a changed lap's fuel prints that lap and exits 1.
func TestAChangedLapsFuelPrintsThatLapAndExitsOne(t *testing.T) {
	changed := clone(raceLaps)
	changed[2].FuelUsed = fp(15.109)
	v1, v2 := sidesWith(raceLaps, changed)
	code, out, errOut := runCLI(t, v1, v2, pitStop())
	if code != exitDiffer {
		t.Fatalf("exit %d, want 1\n%s%s", code, out, errOut)
	}
	if !regexp.MustCompile(`(?m)^\s+1\s+3\s+fuel_used\s+15\.009\s+15\.109\s*$`).MatchString(out) {
		t.Errorf("no line for session 1 lap 3's fuel_used:\n%s", out)
	}
	if strings.Count(out, "fuel_") != 1 {
		t.Errorf("only lap 3's fuel differs:\n%s", out)
	}
	if !strings.Contains(out, "differ: 1 difference") || !strings.Contains(out, "0 of 1 recording match") {
		t.Errorf("output:\n%s", out)
	}
}

// Within R2's 0.001 the fuel is the same.
func TestFuelWithinTheToleranceMatches(t *testing.T) {
	near := clone(raceLaps)
	near[2].FuelUsed = fp(15.010)
	near[0].FuelEnd = fp(65.5984)
	v1, v2 := sidesWith(raceLaps, near)
	if code, out, _ := runCLI(t, v1, v2, pitStop()); code != exitMatch {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
}

// AC3: a recording whose sha256 doesn't match is refused before anything runs.
func TestAShaMismatchIsRefusedBeforeAnythingRuns(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "recordings", pitStop()))
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 0xff
	changed := filepath.Join(t.TempDir(), pitStop())
	if err := os.WriteFile(changed, b, 0o600); err != nil { //nolint:gosec // a path under t.TempDir()
		t.Fatal(err)
	}
	made := false
	var stdout, stderr bytes.Buffer
	code := cli(context.Background(), []string{"laps", pitStop(), changed}, &stdout, &stderr,
		func(options) (Side, Side) { made = true; return nil, nil })
	if code != exitRefused {
		t.Fatalf("exit %d, want 2", code)
	}
	if made || stdout.Len() != 0 {
		t.Errorf("something ran: sides made %v, output %q", made, stdout.String())
	}
	if !strings.Contains(stderr.String(), "refused: sha256") || !strings.Contains(stderr.String(), "SOURCES.yaml says 9684c2ee") {
		t.Errorf("stderr: %s", stderr.String())
	}
}

// AC4: the output names the v1 and v2 images.
func TestTheOutputNamesTheImages(t *testing.T) {
	v1, v2 := sidesWith(raceLaps, clone(raceLaps))
	_, out, _ := runCLI(t, v1, v2, pitStop())
	for _, want := range []string{
		"v1 image: docker.io/muldoon/those-who-run:1.0.0\n",
		"v2 images: ghcr.io/those-who-run/twr-ecu:0.1.4, ghcr.io/those-who-run/twr-logbook:0.1.4\n",
	} {
		if !strings.HasPrefix(out, "v1 image") || !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
}

func TestTheDefaultsAreR4sThree(t *testing.T) {
	v1 := &cannedSide{images: []string{"v1"}, laps: map[string][]Lap{}}
	v2 := &cannedSide{images: []string{"v2"}, laps: map[string][]Lap{}}
	code, out, errOut := runCLI(t, v1, v2)
	if code != exitMatch {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, name := range defaultRecordings {
		if !strings.Contains(out, name+"  sha256 ") {
			t.Errorf("no table for %s:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "3 of 3 recordings match") {
		t.Errorf("output:\n%s", out)
	}
}

func TestAMissingLapAndAnExtraSession(t *testing.T) {
	extra := append(clone(raceLaps[:3]), Lap{Session: 2, Lap: 1, Complete: false, FuelStart: fp(1), FuelEnd: fp(1), FuelUsed: fp(0)})
	v1, v2 := sidesWith(raceLaps, extra)
	code, out, _ := runCLI(t, v1, v2, pitStop())
	if code != exitDiffer {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, re := range []string{
		`(?m)^\s+sessions\s+1\s+2\s*$`,
		`(?m)^\s+laps\s+4\s+4\s*$`,
		`(?m)^\s+1\s+4\s+lap\s+stored\s+missing\s*$`,
		`(?m)^\s+2\s+1\s+lap\s+missing\s+stored\s*$`,
	} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("no %s in:\n%s", re, out)
		}
	}
}

// --frames reports a frame count difference without failing (R7).
func TestFramesAreNotes(t *testing.T) {
	fewer := clone(raceLaps)
	fewer[0].Frames = 117
	v1, v2 := sidesWith(raceLaps, fewer)
	code, out, _ := runCLI(t, v1, v2, "--frames", pitStop())
	if code != exitMatch {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !regexp.MustCompile(`(?m)^\s+1\s+1\s+frames\s+118\s+117\s+\(note\)$`).MatchString(out) {
		t.Errorf("no note:\n%s", out)
	}
	if code, out, _ := runCLI(t, v1, v2, pitStop()); code != exitMatch || strings.Contains(out, "frames") {
		t.Errorf("without --frames: exit %d\n%s", code, out)
	}
}

func TestKeepWritesTheNormalisedLaps(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	v1, v2 := sidesWith(raceLaps, clone(raceLaps))
	_, out, _ := runCLI(t, v1, v2, "--keep", pitStop())
	m := regexp.MustCompile(`normalised laps kept in (\S+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no kept directory:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(m[1], "real-nuc-suzuka-pit-stop-2026-10-15.v2.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(string(b), "\n", 2)[0]
	want := `{"session":1,"lap":1,"lap_time_ms":57109,"complete":true,"fuel_start":96.7237,"fuel_end":65.5994,"fuel_used":31.124,"position":1,"frames":118}`
	if first != want {
		t.Errorf("first line\n%s\nwant\n%s", first, want)
	}
}

func TestUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli(context.Background(), nil, &stdout, &stderr, nil); code != exitRefused {
		t.Errorf("no subcommand: exit %d", code)
	}
	if code := cli(context.Background(), []string{"laps", "--nope"}, &stdout, &stderr, nil); code != exitRefused {
		t.Errorf("bad flag: exit %d", code)
	}
	if code := cli(context.Background(), []string{"laps", "-h"}, &stdout, &stderr, nil); code != exitMatch {
		t.Errorf("-h: exit %d", code)
	}
}

// failingSide fails at one step.
type failingSide struct {
	cannedSide
	imagesErr, lapsErr error
}

func (f *failingSide) Images(ctx context.Context) ([]string, error) {
	if f.imagesErr != nil {
		return nil, f.imagesErr
	}
	return f.cannedSide.Images(ctx)
}

func (f *failingSide) Laps(ctx context.Context, rec Recording, want int) ([]Lap, error) {
	if f.lapsErr != nil {
		return nil, f.lapsErr
	}
	return f.cannedSide.Laps(ctx, rec, want)
}

// A side that fails is an error (exit 2), never a match or a difference.
func TestASideThatFailsExitsTwo(t *testing.T) {
	boom := errors.New("boom")
	ok := cannedSide{images: []string{"img"}, laps: map[string][]Lap{}}
	for name, s := range map[string][2]Side{
		"v1 images": {&failingSide{cannedSide: ok, imagesErr: boom}, &failingSide{cannedSide: ok}},
		"v2 images": {&failingSide{cannedSide: ok}, &failingSide{cannedSide: ok, imagesErr: boom}},
		"v1 laps":   {&failingSide{cannedSide: ok, lapsErr: boom}, &failingSide{cannedSide: ok}},
		"v2 laps":   {&failingSide{cannedSide: ok}, &failingSide{cannedSide: ok, lapsErr: boom}},
	} {
		code, _, errOut := runCLI(t, s[0], s[1], pitStop())
		if code != exitRefused || !strings.Contains(errOut, "boom") {
			t.Errorf("%s: exit %d, stderr %q", name, code, errOut)
		}
	}
}

// Nulls compare as nulls: a lap time or fuel on one side only is a difference.
func TestNulls(t *testing.T) {
	a := []Lap{{Session: 1, Lap: 1, LapTimeMs: nil, FuelUsed: nil, Position: nil}}
	b := []Lap{{Session: 1, Lap: 1, LapTimeMs: ip(1), FuelUsed: fp(0), Position: nil}}
	diffs := compare(a, b, false)
	if len(diffs) != 2 || diffs[0].Field != "lap_time_ms" || diffs[0].V1 != "null" || diffs[1].Field != "fuel_used" || diffs[1].V1 != "null" || diffs[1].V2 != "0.000" {
		t.Errorf("%+v", diffs)
	}
	if d := compare(a, a, false); len(d) != 0 {
		t.Errorf("%+v", d)
	}
}

func TestTheRealSides(t *testing.T) {
	t.Setenv("CONTAINER", "podman")
	t.Setenv("COMPOSE", "podman compose")
	v1, v2 := realSides(&bytes.Buffer{})(options{v1Image: "v1:img", compose: "/c.yaml", keep: true})
	if s := v1.(*v1Side); s.container != "podman" || s.image != "v1:img" || !s.keep {
		t.Errorf("%+v", s)
	}
	if s := v2.(*v2Side); strings.Join(s.compose, " ") != "podman compose" || s.file != "/c.yaml" || s.healthz != "http://127.0.0.1:8080/healthz" {
		t.Errorf("%+v", s)
	}
	if images, _ := v1.Images(context.Background()); len(images) != 1 || images[0] != "v1:img" {
		t.Errorf("%q", images)
	}
	t.Setenv("CONTAINER", "")
	t.Setenv("COMPOSE", "")
	v1, v2 = realSides(&bytes.Buffer{})(options{})
	if v1.(*v1Side).container != "docker" || strings.Join(v2.(*v2Side).compose, " ") != "docker compose" {
		t.Errorf("defaults %+v %+v", v1, v2)
	}
}

func TestTheDefaultV1ImageIsPinned(t *testing.T) {
	if !regexp.MustCompile(`^docker\.io/muldoon/those-who-run:1\.0\.0@sha256:[0-9a-f]{64}$`).MatchString(defaultV1Image) {
		t.Errorf("%s", defaultV1Image)
	}
}
