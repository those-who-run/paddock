package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRunner answers commands by the first rule whose words they contain, and
// records every command.
type fakeRunner struct {
	rules []rule
	calls []string
}

type rule struct {
	has string
	out string
	err error
}

func (f *fakeRunner) run(_ context.Context, argv ...string) ([]byte, error) {
	cmd := strings.Join(argv, " ")
	f.calls = append(f.calls, cmd)
	for _, r := range f.rules {
		if strings.Contains(cmd, r.has) {
			return []byte(r.out), r.err
		}
	}
	return nil, nil
}

func (f *fakeRunner) called(has string) int {
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c, has) {
			n++
		}
	}
	return n
}

var pitRec = Recording{Name: "real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz", Path: "/rec/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz", SHA256: "9684"}

// v1Rows are what v1Dump prints: a solo session, where v1 stored position 1, then
// a race.
const v1Rows = `{"session_id": "20260101-000001", "session_kind": "solo_timed", "lap": 1, "lap_time_ms": 61000, "complete": true, "fuel_start": 100.0, "fuel_end": 100.0, "fuel_used": 0.0, "position": 1, "frames": 120}
{"session_id": "20260101-000001", "session_kind": "solo_timed", "lap": 2, "lap_time_ms": null, "complete": false, "fuel_start": 100.0, "fuel_end": 100.0, "fuel_used": 0.0, "position": 1, "frames": 30}
{"session_id": "20260101-000300", "session_kind": "lap_race", "lap": 1, "lap_time_ms": 57109, "complete": true, "fuel_start": 96.72, "fuel_end": 65.6, "fuel_used": 31.12, "position": 1, "frames": 118}
`

func TestV1ReplaysIntoAVolumeAndReadsTheStore(t *testing.T) {
	f := &fakeRunner{rules: []rule{{has: "--entrypoint python", out: v1Rows}}}
	var log bytes.Buffer
	s := &v1Side{run: f.run, container: "podman", image: "v1:1.0.0", log: &log}
	laps, err := s.Laps(context.Background(), pitRec, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"podman volume rm --force parity-v1-real-nuc-suzuka-pit-stop-2026-10-15",
		"podman volume create parity-v1-real-nuc-suzuka-pit-stop-2026-10-15",
		"podman run --rm -v parity-v1-real-nuc-suzuka-pit-stop-2026-10-15:/data -v /rec/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz:/recording/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz:ro,z v1:1.0.0 replay /recording/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz --store /data --clock 2026-01-01T00:00:00Z",
	}
	for i, w := range want {
		if f.calls[i] != w {
			t.Errorf("call %d\n%s\nwant\n%s", i, f.calls[i], w)
		}
	}
	if !strings.HasSuffix(f.calls[3], "/data/index.sqlite") || f.calls[4] != want[0] {
		t.Errorf("calls %q", f.calls[3:])
	}
	if len(laps) != 3 || laps[2].Session != 2 || laps[0].Session != 1 {
		t.Fatalf("laps %+v", laps)
	}
	// #276: ecu leaves out position in a solo session, where v1 stored 1.
	if laps[0].Position != nil || laps[1].Position != nil {
		t.Errorf("solo positions %v %v, want null", laps[0].Position, laps[1].Position)
	}
	if laps[2].Position == nil || *laps[2].Position != 1 {
		t.Errorf("race position %v, want 1", laps[2].Position)
	}
	if laps[1].LapTimeMs != nil || laps[1].Complete {
		t.Errorf("lap 2 %+v", laps[1])
	}
}

func TestV1KeepsItsVolume(t *testing.T) {
	f := &fakeRunner{}
	var log bytes.Buffer
	s := &v1Side{run: f.run, container: "docker", image: "v1", keep: true, log: &log}
	if _, err := s.Laps(context.Background(), pitRec, 0); err != nil {
		t.Fatal(err)
	}
	if f.called("volume rm") != 1 || !strings.Contains(log.String(), "kept v1's store, volume parity-v1-real-nuc") {
		t.Errorf("calls %q, log %q", f.calls, log.String())
	}
}

func TestV1Fails(t *testing.T) {
	for _, r := range []rule{
		{has: " replay ", err: errors.New("exit 1")},
		{has: "--entrypoint", err: errors.New("no store")},
		{has: "--entrypoint", out: "not json\n"},
		{has: "volume create", err: errors.New("no room")},
	} {
		f := &fakeRunner{rules: []rule{r}}
		s := &v1Side{run: f.run, container: "docker", image: "v1", log: &bytes.Buffer{}}
		if _, err := s.Laps(context.Background(), pitRec, 0); err == nil {
			t.Errorf("%+v: no error", r)
		}
	}
}

// fakeLogbook serves /healthz: 503 for its first answer, then laps_written from n.
func fakeLogbook(n *atomic.Int64) *httptest.Server {
	var asked atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if asked.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprintf(w, `{"status":"ok","laps_written":%d}`, n.Load())
	}))
}

func newV2(f *fakeRunner, url string, keep bool, log *bytes.Buffer) *v2Side {
	return &v2Side{
		run: f.run, compose: []string{"docker", "compose"}, file: "/paddock/deploy/compose/compose.yaml",
		keep: keep, log: log, healthz: url, client: http.DefaultClient,
		wait: 2 * time.Second, settle: 100 * time.Millisecond, quiet: 40 * time.Millisecond, poll: 10 * time.Millisecond,
	}
}

const exported = `{"src":"parity","session_id":"01JB","lap":1,"lap_time_ms":57109,"complete":true,"fuel_start":96.72,"fuel_end":65.6,"fuel_used":31.12,"position":1,"first_seq":1,"last_seq":3000,"frames":118,"file":"x","recorded_at":"2026-01-01T00:01:00Z"}
`

func TestV2ReplaysThroughTheStackAndExports(t *testing.T) {
	var n atomic.Int64
	srv := fakeLogbook(&n)
	defer srv.Close()
	f := &fakeRunner{rules: []rule{{has: "logbook export", out: exported}}}
	s := newV2(f, srv.URL, false, &bytes.Buffer{})
	s.run = func(ctx context.Context, argv ...string) ([]byte, error) {
		if strings.Contains(strings.Join(argv, " "), "replay replay") {
			n.Store(1) // logbook stores the lap once ecu has published it
		}
		return f.run(ctx, argv...)
	}
	laps, err := s.Laps(context.Background(), pitRec, 1)
	if err != nil {
		t.Fatal(err)
	}
	stack := "docker compose -p parity-real-nuc-suzuka-pit-stop-2026-10-15 -f /paddock/deploy/compose/compose.yaml "
	want := []string{
		stack + "down --volumes --remove-orphans",
		stack + "up -d",
		stack + "run --rm --no-deps -T -v /rec/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz:/recording/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz:ro,z replay replay --src parity --speed max /recording/real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz",
		stack + "stop logbook",
		stack + "run --rm --no-deps -T logbook export --data /data --laps --src parity",
		stack + "down --volumes --remove-orphans",
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls\n%s\nwant\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
	if len(laps) != 1 || laps[0].Session != 1 || *laps[0].LapTimeMs != 57109 || laps[0].Frames != 118 {
		t.Errorf("laps %+v", laps)
	}
}

// With fewer laps than v1's, v2 settles and its laps are compared, not waited on.
func TestV2SettlesShortOfV1sLaps(t *testing.T) {
	var n atomic.Int64
	srv := fakeLogbook(&n)
	defer srv.Close()
	f := &fakeRunner{}
	var log bytes.Buffer
	s := newV2(f, srv.URL, true, &log)
	start := time.Now()
	if _, err := s.Laps(context.Background(), pitRec, 6); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %s", time.Since(start))
	}
	if last := f.calls[len(f.calls)-1]; !strings.HasSuffix(last, " stop") {
		t.Errorf("kept stack: last call %q", last)
	}
	if !strings.Contains(log.String(), "kept v2's stack, compose project parity-real-nuc-suzuka-pit-stop-2026-10-15") {
		t.Errorf("log %q", log.String())
	}
}

func TestV2WithNoLogbook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	f := &fakeRunner{}
	s := newV2(f, srv.URL, false, &bytes.Buffer{})
	s.wait = 50 * time.Millisecond
	_, err := s.Laps(context.Background(), pitRec, 0)
	if err == nil || !strings.Contains(err.Error(), "logbook not healthy") {
		t.Fatalf("%v", err)
	}
	if f.called("replay replay") != 0 || !strings.HasSuffix(f.calls[len(f.calls)-1], "down --volumes --remove-orphans") {
		t.Errorf("calls %q", f.calls)
	}
}

func TestV2Images(t *testing.T) {
	f := &fakeRunner{rules: []rule{{has: "config --images", out: "ghcr.io/those-who-run/twr-ecu:0.1.4\nghcr.io/those-who-run/twr-logbook:0.1.4\n\n"}}}
	s := newV2(f, "", false, &bytes.Buffer{})
	images, err := s.Images(context.Background())
	if err != nil || len(images) != 2 || images[0] != "ghcr.io/those-who-run/twr-ecu:0.1.4" {
		t.Fatalf("%q %v", images, err)
	}
	if f.calls[0] != "docker compose -f /paddock/deploy/compose/compose.yaml --profile replay config --images" {
		t.Errorf("%q", f.calls[0])
	}
	if _, err := newV2(&fakeRunner{}, "", false, &bytes.Buffer{}).Images(context.Background()); err == nil {
		t.Error("no images: no error")
	}
}

func TestV2Fails(t *testing.T) {
	var n atomic.Int64
	srv := fakeLogbook(&n)
	defer srv.Close()
	for _, r := range []rule{
		{has: "up -d", err: errors.New("no")},
		{has: "replay replay", err: errors.New("exit 2")},
		{has: "stop logbook", err: errors.New("no")},
		{has: "logbook export", err: errors.New("locked")},
		{has: "logbook export", out: "{}\n"},
	} {
		f := &fakeRunner{rules: []rule{r}}
		if _, err := newV2(f, srv.URL, false, &bytes.Buffer{}).Laps(context.Background(), pitRec, 0); err == nil {
			t.Errorf("%+v: no error", r)
		}
	}
}

func TestExecRunner(t *testing.T) {
	out, err := execRunner(context.Background(), "sh", "-c", "echo out; echo err >&2")
	if err != nil || string(out) != "out\n" {
		t.Errorf("%q %v", out, err)
	}
	_, err = execRunner(context.Background(), "sh", "-c", "echo why >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "why") {
		t.Errorf("%v", err)
	}
}

func TestSafeName(t *testing.T) {
	if got := safeName("Real Race 2laps.v2_"); got != "real-race-2laps-v2" {
		t.Errorf("%q", got)
	}
}

// v2 stores a lap more than v1 (an incomplete last lap, say), just after reaching
// v1's count: the harness waits for logbook to go quiet, so the export has the
// extra lap and the run differs, rather than stopping logbook at v1's count.
func TestV2StoresALapMoreThanV1(t *testing.T) {
	var n atomic.Int64
	srv := fakeLogbook(&n)
	defer srv.Close()
	extra := `{"session_id":"01JB","lap":2,"lap_time_ms":null,"complete":false,"fuel_start":65.6,"fuel_end":60.1,"fuel_used":5.5,"position":1,"frames":20}
`
	f := &fakeRunner{}
	v2 := newV2(f, srv.URL, false, &bytes.Buffer{})
	v2.run = func(ctx context.Context, argv ...string) ([]byte, error) {
		cmd := strings.Join(argv, " ")
		switch {
		case strings.Contains(cmd, "replay replay"):
			n.Store(1)
			go func() {
				time.Sleep(20 * time.Millisecond)
				n.Store(2)
			}()
		case strings.Contains(cmd, "logbook export"):
			if n.Load() == 2 {
				return []byte(exported + extra), nil
			}
			return []byte(exported), nil
		}
		return f.run(ctx, argv...)
	}
	v1 := &cannedSide{images: []string{"v1"}, laps: map[string][]Lap{pitRec.Name: {
		{Session: 1, Lap: 1, LapTimeMs: ip(57109), Complete: true, FuelStart: fp(96.72), FuelEnd: fp(65.6), FuelUsed: fp(31.12), Position: ip(1), Frames: 118},
	}}}
	images := &failingSide{cannedSide: cannedSide{images: []string{"v2"}}}
	var out, errOut bytes.Buffer
	code := compareAll(context.Background(), []Recording{pitRec}, v1, sideOf{images, v2}, options{}, &out, &errOut)
	if code != exitDiffer {
		t.Fatalf("exit %d, want 1\n%s%s", code, out.String(), errOut.String())
	}
	if !regexp.MustCompile(`(?m)^\s+1\s+2\s+lap\s+missing\s+stored\s*$`).MatchString(out.String()) {
		t.Errorf("no extra lap:\n%s", out.String())
	}
}

// sideOf takes its images from one side and its laps from another.
type sideOf struct {
	images Side
	laps   Side
}

func (s sideOf) Images(ctx context.Context) ([]string, error) { return s.images.Images(ctx) }

func (s sideOf) Laps(ctx context.Context, rec Recording, want int) ([]Lap, error) {
	return s.laps.Laps(ctx, rec, want)
}

func (s sideOf) Events(ctx context.Context, run Run) ([]Item, error) {
	return s.laps.Events(ctx, run)
}
