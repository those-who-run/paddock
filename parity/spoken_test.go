package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

const fuji = "real-nuc-fuji-timed-2026-10-15.jsonl.gz"

// fujiV1 is what v1 1.0.0's `replay` prints for the Fuji time trial with
// `script: false`, its log lines among the callouts. The first frame's t is
// 6688369.560608022.
const fujiV1 = `2026-10-30 14:49:36,334 INFO those_who_run.engine: race start: track None
[6688411.02s] (start_brief) Fuji International Speedway.
2026-10-30 14:49:36,339 INFO those_who_run.engine: callout queued (lap_time, p20): Lap one. two oh four point three.
[6688533.43s] (lap_time) Lap one. two oh four point three.
[6688653.86s] (lap_time) Lap two. two oh zero point three. New best by four point zero.
[6688674.80s] (race_summary) Session done. Two laps. Best lap two oh zero point three.
`

// fujiRadio is radio's log for the same, its console lines among its own.
const fujiRadio = `time=2026-10-30T14:00:00.000+00:00 level=INFO msg=ready nats=nats://nats:4222
time=2026-10-30T14:00:00.100+00:00 level=INFO msg="callouts: taking" src=parity stream=CALLOUTS timing=instant
[00:00:41.46] (start_brief) Fuji International Speedway.  kind=template_track id=01A
[00:02:43.87] (lap_time) Lap one. two oh four point three.  kind=template id=01B
[00:04:44.30] (lap_time) Lap two. two oh zero point three. New best by four point zero.  kind=template_new_best id=01C
[00:05:05.24] (race_summary) Session done. Two laps. Best lap two oh zero point three.  kind=template_solo id=01D
`

func fujiLines(t *testing.T) []Line {
	t.Helper()
	return readRadioLines([]byte(fujiRadio))
}

func TestBothConsolesReadAsTheSameLines(t *testing.T) {
	v1 := readV1Lines([]byte(fujiV1), 6688369.560608022)
	v2 := fujiLines(t)
	if len(v1) != 4 || len(v2) != 4 {
		t.Fatalf("v1 %d lines, v2 %d", len(v1), len(v2))
	}
	for i := range v1 {
		if v1[i].Rule != v2[i].Rule || v1[i].Text != v2[i].Text || v1[i].At != v2[i].At {
			t.Errorf("line %d: v1 %+v, v2 %+v", i+1, v1[i], v2[i])
		}
	}
	if v2[0].Kind != "template_track" || v1[0].Kind != "" {
		t.Errorf("kinds %q %q", v1[0].Kind, v2[0].Kind)
	}
}

func fujiRec(t *testing.T) Recording {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "recordings", fuji))
	if err != nil {
		t.Fatal(err)
	}
	return Recording{Name: fuji, Path: path, SHA256: "e3b0"}
}

func TestTheSameListMatches(t *testing.T) {
	r := compareSpoken(fujiRec(t), fujiLines(t), fujiLines(t), nil)
	if r.Matched != 4 || len(r.Diffs) != 0 || r.Failing() != 0 {
		t.Errorf("%+v", r)
	}
}

func TestAPhraseSaidOtherwiseIsOneDifference(t *testing.T) {
	v2 := fujiLines(t)
	v2[1].Text = "Lap one. two oh four point four."
	r := compareSpoken(fujiRec(t), fujiLines(t), v2, nil)
	if r.Matched != 3 || len(r.Diffs) != 1 {
		t.Fatalf("%+v", r)
	}
	d := r.Diffs[0]
	if d.V1 == nil || d.V2 == nil || d.N1 != 2 || d.N2 != 2 || d.Rule() != "lap_time" {
		t.Errorf("%+v", d)
	}
}

func TestACalloutMissingOrExtraIsADifference(t *testing.T) {
	v1 := fujiLines(t)
	extra := Line{Rule: "position", Text: "Up to ninth.", At: 300}
	v2 := append([]Line{}, v1[:2]...)
	v2 = append(v2, extra, v1[3])
	r := compareSpoken(fujiRec(t), v1, v2, nil)
	if r.Matched != 3 || len(r.Diffs) != 2 || r.Failing() != 2 {
		t.Fatalf("%+v", r)
	}
	// In time order: v1's third line (at 284.3 s), then v2's extra one (300 s).
	if r.Diffs[0].V2 != nil || r.Diffs[0].N1 != 3 || r.Diffs[1].V1 != nil || r.Diffs[1].N2 != 3 {
		t.Errorf("%+v %+v", r.Diffs[0], r.Diffs[1])
	}
	// Order matters: the same lines in another order differ.
	swapped := fujiLines(t)
	swapped[1], swapped[2] = swapped[2], swapped[1]
	if r := compareSpoken(fujiRec(t), v1, swapped, nil); r.Failing() == 0 {
		t.Errorf("reordered lines matched: %+v", r)
	}
	// A line radio said twice is one too many.
	twice := append(fujiLines(t), fujiLines(t)[3])
	if r := compareSpoken(fujiRec(t), v1, twice, nil); r.Failing() != 1 {
		t.Errorf("a line said twice: %+v", r)
	}
}

var fujiKnown = []knownDifference{{
	ID: "test-entry", Issue: "those-who-run/paddock#64", Decision: "for the test",
	Recording: "real-nuc-fuji-timed-2026-10-15", Rule: "lap_time",
	V1: "Lap one. two oh four point three.", V2: "",
}}

func TestAKnownDifferenceIsAllowedAndAStaleOneFails(t *testing.T) {
	v2 := fujiLines(t)
	v2 = append(v2[:1], v2[2:]...)
	r := compareSpoken(fujiRec(t), fujiLines(t), v2, fujiKnown)
	if len(r.Diffs) != 1 || r.Diffs[0].Known != "test-entry" || r.Failing() != 0 {
		t.Fatalf("%+v", r)
	}
	if stale := unusedKnown([]SpokenResult{r}, fujiKnown); len(stale) != 0 {
		t.Errorf("used entry stale: %v", stale)
	}
	same := compareSpoken(fujiRec(t), fujiLines(t), fujiLines(t), fujiKnown)
	if stale := unusedKnown([]SpokenResult{same}, fujiKnown); len(stale) != 1 {
		t.Errorf("unused entry not stale: %v", stale)
	}
	// An entry for a recording not compared is not stale.
	other := compareSpoken(Recording{Name: "x.jsonl.gz"}, nil, nil, fujiKnown)
	if stale := unusedKnown([]SpokenResult{other}, fujiKnown); len(stale) != 0 {
		t.Errorf("entry of another recording stale: %v", stale)
	}
	// The text must be the same: another one is a difference to report.
	v2[1].Text = "Lap two."
	if r := compareSpoken(fujiRec(t), fujiLines(t), v2, fujiKnown); r.Failing() != len(r.Diffs) || r.Failing() == 0 {
		t.Errorf("%+v", r)
	}
}

func TestTheKnownListIsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, k := range knownSpoken {
		if k.ID == "" || ids[k.ID] || !strings.HasPrefix(k.Issue, "those-who-run/") || k.Decision == "" ||
			k.Recording == "" || strings.HasSuffix(k.Recording, ".gz") || k.Rule == "" || (k.V1 == "" && k.V2 == "") {
			t.Errorf("entry %+v", k)
		}
		ids[k.ID] = true
	}
}

func TestADifferenceIsShownWithItsFrame(t *testing.T) {
	ts, err := frameTimes(fujiRec(t).Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 633 {
		t.Errorf("%d frames", len(ts))
	}
	l := fujiLines(t)[0]
	n := frameAt(ts, SpokenDiff{V2: &l})
	if n < 2 || ts[n-1]-ts[0] < 41.455 || ts[n-2]-ts[0] >= 41.455 {
		t.Errorf("frame %d", n)
	}
	if n := frameAt(ts, SpokenDiff{V1: &Line{At: 1e9}}); n != len(ts) {
		t.Errorf("past the end: frame %d", n)
	}
}

func TestFrameTimesRefusesABadRecording(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"empty.jsonl": "", "no-t.jsonl": `{"phase":"menu"}` + "\n"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := frameTimes(p); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := frameTimes(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Error("missing: no error")
	}
	notGz := filepath.Join(dir, "plain.jsonl.gz")
	if err := os.WriteFile(notGz, []byte(`{"t":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := frameTimes(notGz); err == nil {
		t.Error("not gzip: no error")
	}
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	_, _ = gz.Write([]byte("{\"t\":5}\n{\"t\":6}"))
	_ = gz.Close()
	ok := filepath.Join(dir, "ok.jsonl.gz")
	if err := os.WriteFile(ok, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if ts, err := frameTimes(ok); err != nil || len(ts) != 2 {
		t.Errorf("%v %v", ts, err)
	}
}

func TestV1SpeaksWithTheScriptOff(t *testing.T) {
	f := &fakeRunner{rules: []rule{{has: " replay ", out: fujiV1}}}
	s := &v1Side{run: f.run, container: "podman", image: "v1:1.0.0"}
	lines, err := s.Spoken(context.Background(), fujiRec(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 4 || lines[0].At != 41.46 {
		t.Errorf("%+v", lines)
	}
	call := f.calls[0]
	for _, want := range []string{"podman run --rm -e GT7_RULES_FILE=/rules/script-false.yaml -v ", ":/rules:ro,z -v ",
		":/recording/" + fuji + ":ro,z v1:1.0.0 replay /recording/" + fuji} {
		if !strings.Contains(call, want) {
			t.Errorf("%s\nwants %s", call, want)
		}
	}
	f.rules[0].err = errors.New("boom")
	if _, err := s.Spoken(context.Background(), fujiRec(t)); err == nil || !strings.Contains(err.Error(), "v1 replay") {
		t.Errorf("%v", err)
	}
	if _, err := s.Spoken(context.Background(), Recording{Name: "x", Path: "/nowhere"}); err == nil {
		t.Error("a missing recording: no error")
	}
}

func TestTheOverrideTurnsTheScriptOffInRadio(t *testing.T) {
	o := radioOverride("/tmp/rules")
	for _, want := range []string{"TWR_RADIO_SRC: parity", "TWR_RADIO_SINK: console", "TWR_RADIO_TIMING: instant",
		"TWR_RADIO_VOICE_FILE: /rules/script-false.yaml", `source: "/tmp/rules/script-false.yaml"`} {
		if !strings.Contains(o, want) {
			t.Errorf("no %q in\n%s", want, o)
		}
	}
	dir, err := rulesDir()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	b, err := os.ReadFile(filepath.Join(dir, "script-false.yaml")) //nolint:gosec // the test's own file
	if err != nil || string(b) != scriptOff {
		t.Errorf("%q %v", b, err)
	}
}

func spokenV2(t *testing.T, logs string, logErr error) (*v2Side, *fakeRunner, func()) {
	t.Helper()
	var n atomic.Int64
	srv := fakeLogbook(&n)
	f := &fakeRunner{rules: []rule{{has: " logs ", out: logs, err: logErr}}}
	s := newV2(f, srv.URL, false, &bytes.Buffer{})
	s.said = 30 * s.poll
	return s, f, srv.Close
}

func TestV2SpeaksThroughRadiosConsole(t *testing.T) {
	s, f, done := spokenV2(t, fujiRadio, nil)
	defer done()
	lines, err := s.Spoken(context.Background(), fujiRec(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 4 || lines[3].Rule != "race_summary" {
		t.Errorf("%+v", lines)
	}
	project := "-p parity-real-nuc-fuji-timed-2026-10-15 -f /paddock/deploy/compose/compose.yaml -f "
	order := []string{"--profile replay down --volumes", " up -d", "--profile replay up -d --no-deps radio",
		"--profile replay logs --no-color --no-log-prefix radio", "run --rm --no-deps -T -v ", "--profile replay down --volumes"}
	at := 0
	for _, want := range order {
		for at < len(f.calls) && !strings.Contains(f.calls[at], want) {
			at++
		}
		if at == len(f.calls) {
			t.Fatalf("no %q in order in\n%s", want, strings.Join(f.calls, "\n"))
		}
	}
	for _, c := range f.calls {
		if !strings.Contains(c, project) || !strings.Contains(c, "radio.yaml") {
			t.Errorf("not the project's stack with the override: %s", c)
		}
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "replay replay --src parity --speed max /recording/"+fuji) {
		t.Errorf("no replay in\n%s", strings.Join(f.calls, "\n"))
	}
}

func TestV2KeepsTheStackWhenAsked(t *testing.T) {
	s, f, done := spokenV2(t, fujiRadio, nil)
	defer done()
	s.keep = true
	if _, err := s.Spoken(context.Background(), fujiRec(t)); err != nil {
		t.Fatal(err)
	}
	if last := f.calls[len(f.calls)-1]; !strings.HasSuffix(last, "--profile replay stop") {
		t.Errorf("last call %s", last)
	}
}

func TestV2FailsWhenRadioStops(t *testing.T) {
	stopped := fujiRadio + "time=2026-10-30T14:00:09.000+00:00 level=INFO msg=\"shutting down\"\n"
	s, _, done := spokenV2(t, stopped, nil)
	defer done()
	if _, err := s.Spoken(context.Background(), fujiRec(t)); err == nil || !strings.Contains(err.Error(), "stopped during the replay") {
		t.Errorf("%v", err)
	}
	never := "time=2026-10-30T14:00:00.000+00:00 level=ERROR msg=stopped err=boom\n"
	s, _, done2 := spokenV2(t, never, nil)
	defer done2()
	if _, err := s.Spoken(context.Background(), fujiRec(t)); err == nil || !strings.Contains(err.Error(), "not taking callouts") {
		t.Errorf("%v", err)
	}
}

func TestV2FailsWhenAStepFails(t *testing.T) {
	for _, step := range []string{"down --volumes", " up -d", "up -d --no-deps radio", "replay replay"} {
		s, f, done := spokenV2(t, fujiRadio, nil)
		f.rules = append([]rule{{has: step, err: errors.New("boom")}}, f.rules...)
		if _, err := s.Spoken(context.Background(), fujiRec(t)); err == nil {
			t.Errorf("%s: no error", step)
		}
		done()
	}
	// The logs can't be read: radio never takes callouts, in s.wait.
	s, _, done := spokenV2(t, "", errors.New("no logs"))
	defer done()
	if _, err := s.Spoken(context.Background(), fujiRec(t)); err == nil {
		t.Error("no logs: no error")
	}
	// The logs fail after the replay.
	s, f, done3 := spokenV2(t, fujiRadio, nil)
	defer done3()
	n := 0
	f.rules = append([]rule{{has: " logs ", err: errors.New("gone")}}, f.rules...)
	f.rules[0].has = "never matches"
	wrapped := s.run
	s.run = func(ctx context.Context, argv ...string) ([]byte, error) {
		if strings.Contains(strings.Join(argv, " "), " logs ") {
			if n++; n > 1 {
				return nil, errors.New("gone")
			}
		}
		return wrapped(ctx, argv...)
	}
	if _, err := s.Spoken(context.Background(), fujiRec(t)); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("%v", err)
	}
}

// cannedSpeaker speaks canned lines, by recording name.
type cannedSpeaker struct {
	cannedSide
	lines map[string][]Line
	err   error
}

func (c *cannedSpeaker) Spoken(_ context.Context, rec Recording) ([]Line, error) {
	return c.lines[rec.Name], c.err
}

func runSpoken(t *testing.T, v1, v2 Side, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli(context.Background(), append([]string{"spoken"}, args...), &stdout, &stderr,
		func(options) (Side, Side) { return v1, v2 })
	return code, stdout.String(), stderr.String()
}

func speakers(v1, v2 []Line) (*cannedSpeaker, *cannedSpeaker) {
	return &cannedSpeaker{cannedSide: cannedSide{images: []string{"v1"}}, lines: map[string][]Line{fuji: v1}},
		&cannedSpeaker{cannedSide: cannedSide{images: []string{"ecu", "radio"}}, lines: map[string][]Line{fuji: v2}}
}

func TestSpokenMatchesAndPrintsTheTable(t *testing.T) {
	v1, v2 := speakers(fujiLines(t), fujiLines(t))
	code, out, errOut := runSpoken(t, v1, v2, fuji)
	if code != exitMatch {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, want := range []string{"v2 images: ecu, radio", "match: 4 callouts", "parity spoken: 1 of 1 recording match"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}

func TestSpokenFailsOnADifferenceAndShowsTheFrame(t *testing.T) {
	v2Lines := fujiLines(t)
	v2Lines[1].Text = "Lap one."
	v1, v2 := speakers(fujiLines(t), v2Lines)
	code, out, _ := runSpoken(t, v1, v2, fuji)
	if code != exitDiffer {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !regexp.MustCompile(`\d+\s+163\.87 s\s+lap_time\s+#2 "Lap one. two oh four point three."\s+#2 "Lap one."`).MatchString(out) {
		t.Errorf("no difference line in\n%s", out)
	}
	if !strings.Contains(out, "differ: 1 difference") || !strings.Contains(out, "0 of 1 recording match") {
		t.Errorf("%s", out)
	}
}

func TestSpokenKeepsBothSidesLines(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	v1, v2 := speakers(fujiLines(t), fujiLines(t))
	_, out, _ := runSpoken(t, v1, v2, "--keep", fuji)
	m := regexp.MustCompile(`spoken lines kept in (\S+)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no kept directory:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(m[1], "real-nuc-fuji-timed-2026-10-15.spoken.v2.jsonl")) //nolint:gosec // the test's own directory
	if err != nil || !strings.Contains(string(b), `"kind":"template_track"`) {
		t.Errorf("%s %v", b, err)
	}
}

func TestSpokenRefuses(t *testing.T) {
	v1, v2 := speakers(nil, nil)
	if code, _, _ := runSpoken(t, v1, v2, "--all", fuji); code != exitRefused {
		t.Errorf("--all with a recording: exit %d", code)
	}
	if code, _, _ := runSpoken(t, v1, v2, "nowhere-at-all"); code != exitRefused {
		t.Errorf("a missing recording: exit %d", code)
	}
	if code, _, _ := runSpoken(t, &cannedSide{}, v2, fuji); code != exitRefused {
		t.Errorf("a side that doesn't speak: exit %d", code)
	}
	if code, _, _ := runSpoken(t, v1, v2, "--bad"); code != exitRefused {
		t.Errorf("a bad flag: exit %d", code)
	}
	v1.err = errors.New("boom")
	if code, _, errOut := runSpoken(t, v1, v2, fuji); code != exitRefused || !strings.Contains(errOut, "v1: boom") {
		t.Errorf("v1 fails: exit %d %s", code, errOut)
	}
	v1.err, v2.err = nil, errors.New("boom")
	if code, _, errOut := runSpoken(t, v1, v2, fuji); code != exitRefused || !strings.Contains(errOut, "v2: boom") {
		t.Errorf("v2 fails: exit %d %s", code, errOut)
	}
}

func TestSpokenDefaultsToTheNineAndAllIsEverything(t *testing.T) {
	v1, v2 := speakers(nil, nil)
	_, out, _ := runSpoken(t, v1, v2)
	if !strings.Contains(out, "parity spoken: 9 of 9 recordings match") {
		t.Errorf("%s", out)
	}
	_, out, _ = runSpoken(t, v1, v2, "--all")
	if !regexp.MustCompile(`parity spoken: (\d\d) of (\d\d) recordings match`).MatchString(out) || strings.Contains(out, " 9 of 9") {
		t.Errorf("%s", out)
	}
}
