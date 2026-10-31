package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pitwallv1 "github.com/those-who-run/contracts/gen/go/twr/pitwall/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// testdata/pitwall/v1.jsonl is what v1pitwall.py printed for #133's three
// recordings through v1 1.0.0, each on its wallClocks clock.
func v1Answers(t *testing.T) []wallAnswer {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "pitwall", "v1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := readWallAnswers(b)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func wallRecs() []Recording {
	recs := make([]Recording, len(wallRecordings))
	for i, name := range wallRecordings {
		recs[i] = Recording{Name: name, Path: "/rec/" + name, SHA256: "ab"}
	}
	return recs
}

// asPitwall is v1's answer as v2's pit wall would send it, if it answered the
// same: v1's body mapped back to twr.pitwall.v1 by contracts' mapping, the
// inverse of v1Shape, written apart from it.
func asPitwall(t *testing.T, a wallAnswer) proto.Message {
	t.Helper()
	var body map[string]any
	b, _ := json.Marshal(a.Body)
	_ = json.Unmarshal(b, &body)
	session := func(s map[string]any) {
		s["best_lap"], s["fuel_burned"] = s["best_lap_no"], s["fuel_used"]
		delete(s, "best_lap_no")
		delete(s, "fuel_used")
		tr := s["track"].(map[string]any)
		tr["track_id"] = tr["id"]
		delete(tr, "id")
		if s["total_laps"] == 0.0 {
			delete(s, "total_laps")
		}
		for _, k := range []string{"started_at", "ended_at"} {
			if f, ok := s[k].(float64); ok {
				sec, frac := math.Modf(f)
				s[k] = time.Unix(int64(sec), int64(frac*1e9)).UTC().Format(time.RFC3339Nano)
			}
		}
	}
	var m proto.Message
	switch a.Call {
	case "sessions", "same_track":
		for _, x := range body["sessions"].([]any) {
			session(x.(map[string]any))
		}
		m = &pitwallv1.ListSessionsResponse{}
		if a.Call == "same_track" {
			m = &pitwallv1.ListSameTrackResponse{}
		}
	case "session":
		session(body)
		body, m = map[string]any{"session": body}, &pitwallv1.GetSessionResponse{}
	case "laps":
		for _, l := range body["laps"].([]any) {
			delete(l.(map[string]any), "file")
		}
		m = &pitwallv1.ListLapsResponse{}
	case "trace":
		body, m = map[string]any{"trace": body}, &pitwallv1.GetTraceResponse{}
	}
	b, _ = json.Marshal(body)
	if err := protojson.Unmarshal(b, m); err != nil {
		t.Fatalf("%s: %v", a.Call, err)
	}
	return m
}

// v2Answers are v1's answers as v2 would give them, through the wire's JSON
// (protojson's own names) and back through v1Shape.
func v2Answers(t *testing.T, v1 []wallAnswer) []wallAnswer {
	t.Helper()
	out := make([]wallAnswer, len(v1))
	for i, a := range v1 {
		wire, err := protojson.Marshal(asPitwall(t, a))
		if err != nil {
			t.Fatal(err)
		}
		m := proto.Clone(asPitwall(t, a))
		proto.Reset(m)
		if err := protojson.Unmarshal(wire, m); err != nil {
			t.Fatal(err)
		}
		body, err := v1Shape(m)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = wallAnswer{Call: a.Call, SessionID: "v2-" + a.SessionID, Lap: a.Lap, Body: renameIDs(body)}
	}
	return out
}

// renameIDs gives v2's answers ids of their own, as ecu mints its own.
func renameIDs(v map[string]any) map[string]any {
	b, _ := json.Marshal(v)
	s := strings.ReplaceAll(string(b), `"session_id":"2026`, `"session_id":"v2-2026`)
	var out map[string]any
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// find is the answer to call about the n-th session of v1's list, by start.
func find(answers []wallAnswer, call, id string, lap int) *wallAnswer {
	for i := range answers {
		if a := &answers[i]; a.Call == call && strings.HasSuffix(a.SessionID, id) && a.Lap == lap {
			return a
		}
	}
	return nil
}

const pitStopID = "20260102-000000"

func TestPitwallsAnswersInV1sShapeMatchV1s(t *testing.T) {
	v1 := v1Answers(t)
	r := compareWall(wallRecs(), v1, v2Answers(t, v1))
	if len(r.Diffs) != 0 {
		t.Fatalf("%+v", r.Diffs)
	}
	c := r.Counts["real-nuc-gilles-villeneuve-pit-stop-2026-10-17"]
	if c.Sessions != [2]int{1, 1} || c.Laps != [2]int{5, 5} {
		t.Errorf("counts %+v", c)
	}
}

// #122 AC2 and AC3 hold on v1's side, so #133's run checks them on v2's.
func TestV1sAnswersHoldTheMovedACs(t *testing.T) {
	v1 := v1Answers(t)
	laps := find(v1, "laps", pitStopID, 0).Body["laps"].([]any)
	flag := func(n int, f string) any { return laps[n-5].(map[string]any)[f] }
	if flag(6, "pit") != true || flag(7, "out_lap") != true || flag(9, "invalid") != true || flag(9, "complete") != false {
		t.Errorf("the Gilles pit stop's laps %v", laps)
	}
	if j := find(v1, "session", "20260103-000000", 0).Body["joined_on_lap"]; j != 7.0 {
		t.Errorf("Suzuka joined on %v", j)
	}
	if s := longestStep(find(v1, "trace", pitStopID, 6).Body); s < 100 {
		t.Errorf("the pit lap's jump %.1f m", s)
	}
}

// #133 AC2: a changed lap flag rule fails the run and names the field.
func TestAChangedLapFlagFailsAndIsNamed(t *testing.T) {
	v1 := v1Answers(t)
	v2 := v2Answers(t, v1)
	laps := find(v2, "laps", pitStopID, 0).Body["laps"].([]any)
	laps[1].(map[string]any)["pit"] = false
	code, out, _ := runWall(t, v1, v2, nil)
	if code != exitDiffer {
		t.Errorf("exit %d", code)
	}
	if !regexpLine(out, `real-nuc-gilles-villeneuve-pit-stop-2026-10-17\s+#1\s+laps\s+6\s+pit\s+true\s+false`) {
		t.Errorf("output\n%s", out)
	}
	// The pit lap's trace no longer has v1's pit flag behind it on v2, but v1's
	// laps say which lap is the pit lap, so its jump is still checked.
	if strings.Contains(out, "jump") {
		t.Errorf("output\n%s", out)
	}
}

func regexpLine(out, pattern string) bool {
	for _, line := range strings.Split(out, "\n") {
		if ok, _ := regexp.MatchString(pattern, line); ok {
			return true
		}
	}
	return false
}

// cannedWall is a side with its answers already given.
type cannedWall struct {
	images  []string
	answers []wallAnswer
	err     error
	want    int
}

func (c *cannedWall) WallImages(context.Context) ([]string, error) { return c.images, nil }
func (c *cannedWall) PitWall(_ context.Context, _ []Recording, clocks []time.Time, want int) ([]wallAnswer, error) {
	c.want = want
	if len(clocks) != 3 || clocks[2].Sub(clocks[0]) != 48*time.Hour {
		return nil, fmt.Errorf("clocks %v", clocks)
	}
	return c.answers, c.err
}

// Sides are Side and PitWall both, as the real ones are.
type wallSides struct {
	cannedSide
	cannedWall
}

func runWall(t *testing.T, v1, v2 []wallAnswer, entries []wallAcceptance, args ...string) (int, string, string) {
	t.Helper()
	saved := wallAccepted
	wallAccepted = entries
	t.Cleanup(func() { wallAccepted = saved })
	var stdout, stderr bytes.Buffer
	a := &wallSides{cannedWall: cannedWall{images: []string{"v1:1.0.0"}, answers: v1}}
	b := &wallSides{cannedWall: cannedWall{images: []string{"twr-pitwall:0.1.0"}, answers: v2}}
	code := cli(context.Background(), append([]string{"pitwall", "--compose", "/c.yaml"}, args...), &stdout, &stderr,
		func(options) (Side, Side) { return a, b })
	if code == exitMatch && b.want != 10 {
		t.Errorf("v2 was told to wait for %d laps", b.want)
	}
	return code, stdout.String(), stderr.String()
}

func TestTheSameAnswersMatch(t *testing.T) {
	v1 := v1Answers(t)
	code, out, errOut := runWall(t, v1, v2Answers(t, v1), nil)
	if code != exitMatch || !strings.Contains(out, "parity pitwall: match, 0 accepted differences") || errOut != "" {
		t.Errorf("exit %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "v1 images: v1:1.0.0") || !strings.Contains(out, "v2 images: twr-pitwall:0.1.0") {
		t.Errorf("%s", out)
	}
}

func TestAnAcceptedDifferenceAndAStaleEntry(t *testing.T) {
	v1 := v1Answers(t)
	v2 := v2Answers(t, v1)
	find(v2, "session", pitStopID, 0).Body["title"] = "Circuit Gilles Villeneuve · 12-lap race!"
	entry := wallAcceptance{
		ID: "a-title", Issue: "#133", Rec: "real-nuc-gilles-villeneuve-pit-stop-2026-10-17", Session: 1,
		Calls: []string{"session"}, Field: "title",
		V1: "Circuit Gilles Villeneuve · 12-lap race", V2: "Circuit Gilles Villeneuve · 12-lap race!",
	}
	code, out, _ := runWall(t, v1, v2, []wallAcceptance{entry})
	if code != exitMatch || !strings.Contains(out, "accepted: a-title") || !strings.Contains(out, "match, 1 accepted difference") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// The same entry with another value allows nothing, and goes stale.
	entry.V2 = "something else"
	code, out, _ = runWall(t, v1, v2, []wallAcceptance{entry})
	if code != exitDiffer || !strings.Contains(out, "accepted.go: a-title (#133) allowed no difference: remove it") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestSessionsPairByTheirStart(t *testing.T) {
	v1 := v1Answers(t)
	v2 := v2Answers(t, v1)
	s := find(v2, "session", pitStopID, 0).Body
	s["started_at"] = s["started_at"].(float64) + 0.9 // within R3's 1 s
	r := compareWall(wallRecs(), v1, v2)
	if len(r.Diffs) != 0 {
		t.Fatalf("%+v", r.Diffs)
	}
	s["started_at"] = s["started_at"].(float64) + 3
	r = compareWall(wallRecs(), v1, v2)
	if len(r.Diffs) != 1 || r.Diffs[0].Call != "session" || r.Diffs[0].Field != "started_at" {
		t.Errorf("%+v", r.Diffs)
	}
}

func TestASessionOnOneSideOnly(t *testing.T) {
	v1 := v1Answers(t)
	v2 := v2Answers(t, v1)
	list := find(v2, "sessions", "", 0).Body
	sessions := list["sessions"].([]any)
	list["sessions"], list["total"], list["matched"] = sessions[1:], 2.0, 2.0
	r := compareWall(wallRecs(), v1, v2)
	fields := map[string]string{}
	for _, d := range r.Diffs {
		fields[d.Field] = d.Rec + " " + d.V1 + "/" + d.V2
	}
	if fields["session"] != "real-nuc-suzuka-joined-mid-race-2026-10-18 stored/missing" || fields["total"] != " 3/2" || fields["order"] == "" {
		t.Errorf("%v", fields)
	}
	if r.Counts["real-nuc-suzuka-joined-mid-race-2026-10-18"].Sessions != [2]int{1, 0} {
		t.Errorf("%+v", r.Counts)
	}
}

func TestTheListsOwnFields(t *testing.T) {
	v1 := v1Answers(t)
	v2 := v2Answers(t, v1)
	opts := find(v2, "sessions", "", 0).Body["options"].(map[string]any)
	opts["track"] = []any{}
	same := find(v2, "same_track", pitStopID, 0).Body
	same["sessions"] = []any{}
	r := compareWall(wallRecs(), v1, v2)
	if len(r.Diffs) != 2 || r.Diffs[0].Field != "options.track" || r.Diffs[0].Rec != "" ||
		r.Diffs[1].Call != "same_track" || r.Diffs[1].V1 != "real-nuc-gilles-villeneuve-race-2026-10-17#1" || r.Diffs[1].V2 != "" {
		t.Errorf("%+v", r.Diffs)
	}
	var out bytes.Buffer
	_ = writeWallResult(&out, r)
	if !strings.Contains(out.String(), "the list") {
		t.Errorf("%s", out.String())
	}
}

func TestTraces(t *testing.T) {
	v1 := v1Answers(t)
	v2 := v2Answers(t, v1)
	tr := find(v2, "trace", pitStopID, 6).Body
	speed := tr["speed_kph"].([]any)
	speed[10] = speed[10].(float64) + 0.1
	tr["gear"] = tr["gear"].([]any)[1:]
	xs, zs := tr["x"].([]any), tr["z"].([]any)
	for i := range xs {
		xs[i], zs[i] = 0.0, float64(i)
	}
	var kept []wallAnswer
	for _, a := range v2 {
		if !(a.Call == "trace" && a.Lap == 8 && strings.HasSuffix(a.SessionID, pitStopID)) {
			kept = append(kept, a)
		}
	}
	r := compareWall(wallRecs(), v1, kept)
	got := map[string]string{}
	for _, d := range r.Diffs {
		got[fmt.Sprintf("%d %s", d.Lap, d.Field)] = d.V1 + " | " + d.V2
	}
	if !strings.HasPrefix(got["6 speed_kph"], "1 of ") || !strings.Contains(got["6 speed_kph"], "first [10]") {
		t.Errorf("%v", got)
	}
	if !strings.HasSuffix(got["6 gear"], "points") || got["6 jump"] != "a pit lap, over 20 m | 1.0 m at most" {
		t.Errorf("%v", got)
	}
	if got["8 trace"] != "traced | not traced" {
		t.Errorf("%v", got)
	}
}

func TestLaps(t *testing.T) {
	v1 := v1Answers(t)
	v2 := v2Answers(t, v1)
	laps := find(v2, "laps", pitStopID, 0).Body
	ls := laps["laps"].([]any)
	ls[0].(map[string]any)["fuel_used"] = ls[0].(map[string]any)["fuel_used"].(float64) + 0.0009 // within 0.001 L
	laps["laps"] = ls[:4]                                                                        // lap 9 gone
	r := compareWall(wallRecs(), v1, v2)
	if len(r.Diffs) != 1 || r.Diffs[0].Lap != 9 || r.Diffs[0].Field != "lap" || r.Diffs[0].V2 != "missing" {
		t.Errorf("%+v", r.Diffs)
	}
}

func TestAnswersThatCantBeRead(t *testing.T) {
	if _, err := readWallAnswers([]byte("{\n")); err == nil {
		t.Error("bad JSON read")
	}
	if _, err := readWallAnswers([]byte(`{"call":"x"}` + "\n")); err == nil {
		t.Error("no body read")
	}
	if _, err := indexWall([]wallAnswer{{Call: "nope", Body: map[string]any{}}}); err == nil {
		t.Error("an unknown call indexed")
	}
	if _, err := indexWall(nil); err == nil {
		t.Error("no list indexed")
	}
	bad := []wallAnswer{{Call: "sessions", Body: map[string]any{"sessions": []any{map[string]any{"session_id": "x"}}}}}
	if _, err := indexWall(bad); err == nil {
		t.Error("a session with no start indexed")
	}
	r := compareWall(wallRecs(), bad, bad)
	if len(r.Diffs) != 1 || r.Diffs[0].Field != "answers" {
		t.Errorf("%+v", r.Diffs)
	}
}

func TestTheWallCommandRefuses(t *testing.T) {
	var stdout, stderr bytes.Buffer
	canned := func(options) (Side, Side) { return &cannedSide{}, &cannedSide{} }
	if code := cli(context.Background(), []string{"pitwall", "--compose", "/c.yaml"}, &stdout, &stderr, canned); code != exitRefused || !strings.Contains(stderr.String(), "no pit wall") {
		t.Errorf("exit %d %s", code, stderr.String())
	}
	if code := cli(context.Background(), []string{"pitwall", "-h"}, &stdout, &stderr, nil); code != exitMatch {
		t.Errorf("-h: exit %d", code)
	}
	if code := cli(context.Background(), []string{"pitwall", "--compose", "/c.yaml", "/nowhere.jsonl.gz"}, &stdout, &stderr, nil); code != exitRefused {
		t.Errorf("a missing recording: exit %d", code)
	}
	// No laps on v1's side, or a side that fails, is refused.
	for name, sides := range map[string][2][]wallAnswer{
		"no laps": {{{Call: "sessions", Body: map[string]any{"sessions": []any{}}}}, nil},
	} {
		code, _, errOut := runWall(t, sides[0], sides[1], nil)
		if code != exitRefused || !strings.Contains(errOut, "lists no laps") {
			t.Errorf("%s: exit %d %s", name, code, errOut)
		}
	}
	saved := wallAccepted
	defer func() { wallAccepted = saved }()
	boom := errors.New("boom")
	for i := range 2 {
		a := &wallSides{cannedWall: cannedWall{answers: v1Answers(t)}}
		b := &wallSides{cannedWall: cannedWall{}}
		[]*wallSides{a, b}[i].err = boom
		code := cli(context.Background(), []string{"pitwall", "--compose", "/c.yaml"}, &stdout, &stderr,
			func(options) (Side, Side) { return a, b })
		if code != exitRefused {
			t.Errorf("side %d fails: exit %d", i+1, code)
		}
	}
}

func TestKeepWritesBothSidesAnswers(t *testing.T) {
	v1 := v1Answers(t)
	code, out, _ := runWall(t, v1, v2Answers(t, v1), nil, "--keep")
	_, dir, ok := strings.Cut(out, "answers kept in ")
	dir = strings.TrimSpace(dir)
	if code != exitMatch || !ok {
		t.Fatalf("exit %d\n%s", code, out)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	b, err := os.ReadFile(filepath.Join(dir, "pitwall.v2.jsonl"))
	if err != nil || bytes.Count(b, []byte("\n")) != len(v1) {
		t.Errorf("%v %d lines", err, bytes.Count(b, []byte("\n")))
	}
}

// v1Shape maps every response the pit wall answers, by contracts' mapping.
func TestV1ShapeMapsContractsNames(t *testing.T) {
	m := &pitwallv1.GetSessionResponse{Session: &pitwallv1.Session{
		SessionId: "01J", BestLap: proto.Int32(3), FuelBurned: proto.Float64(1.5),
		Track: &pitwallv1.Track{TrackId: proto.Int32(10), Name: "Suzuka Circuit", Sure: true},
	}}
	body, err := v1Shape(m)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"best_lap_no": 3.0, "fuel_used": 1.5, "total_laps": 0.0, "started_at": nil, "car": nil, "closed": false}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s: %v, want %v", k, body[k], v)
		}
	}
	for _, k := range []string{"best_lap", "fuel_burned", "card_posted_at", "from_v1_rows"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s is there", k)
		}
	}
	if tr := body["track"].(map[string]any); tr["id"] != 10.0 || tr["evidence"] != nil {
		t.Errorf("track %v", tr)
	}
	if _, err := v1Shape(&pitwallv1.GetTraceResponse{}); err == nil {
		t.Error("a trace response with no trace mapped")
	}
	if err := v1Session(map[string]any{"started_at": "yesterday"}); err == nil {
		t.Error("a bad timestamp mapped")
	}
}

func TestV1AsksItsPitWallInItsImage(t *testing.T) {
	v1 := v1Answers(t)
	var lines bytes.Buffer
	for _, a := range v1 {
		b, _ := json.Marshal(a)
		lines.Write(append(b, '\n'))
	}
	f := &fakeRunner{rules: []rule{{has: "--entrypoint python", out: lines.String()}}}
	s := &v1Side{run: f.run, container: "docker", image: "v1:1.0.0", log: io.Discard}
	got, err := s.PitWall(context.Background(), wallRecs(), wallClocks(3), 0)
	if err != nil || len(got) != len(v1) {
		t.Fatalf("%v, %d answers", err, len(got))
	}
	call := f.calls[0]
	for _, part := range []string{
		"docker run --rm -v /rec/real-nuc-gilles-villeneuve-race-2026-10-17.jsonl.gz:/recording/real-nuc-gilles-villeneuve-race-2026-10-17.jsonl.gz:ro,z",
		"--entrypoint python v1:1.0.0 -c ",
		"--clock 2026-01-01T00:00:00Z --clock 2026-01-02T00:00:00Z --clock 2026-01-03T00:00:00Z /recording/real-nuc-gilles-villeneuve-race-2026-10-17.jsonl.gz /recording/real-nuc-gilles-villeneuve-pit-stop-2026-10-17.jsonl.gz",
	} {
		if !strings.Contains(call, part) {
			t.Errorf("call %q\nwants %q", call, part)
		}
	}
	if images, _ := s.WallImages(context.Background()); len(images) != 1 {
		t.Errorf("%q", images)
	}
	for _, r := range []rule{{has: "run", err: errors.New("exit 1")}, {has: "run", out: "nope\n"}} {
		f := &fakeRunner{rules: []rule{r}}
		s := &v1Side{run: f.run, container: "docker", image: "v1", log: io.Discard}
		if _, err := s.PitWall(context.Background(), wallRecs(), wallClocks(3), 0); err == nil {
			t.Errorf("%+v: no error", r)
		}
	}
}

// fakePitwall is the pit wall over a logbook that has v1's answers as v2's: its
// /healthz, and PitwallService's methods in Connect's JSON. Every session is open
// until closed is set.
func fakePitwall(t *testing.T, closed *atomic.Bool) *httptest.Server {
	t.Helper()
	v1 := v1Answers(t)
	byKey := map[string]proto.Message{}
	for _, a := range v1 {
		byKey[fmt.Sprintf("%s %s %d", a.Call, a.SessionID, a.Lap)] = asPitwall(t, a)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = io.WriteString(w, `{"ok":true,"logbook":true}`)
			return
		}
		method := strings.TrimPrefix(r.URL.Path, "/twr.pitwall.v1.PitwallService/")
		var req struct {
			SessionID string `json:"sessionId"`
			Lap       int    `json:"lap"`
			Points    int    `json:"points"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		call := map[string]string{"ListSessions": "sessions", "GetSession": "session", "ListLaps": "laps", "ListSameTrack": "same_track", "GetTrace": "trace"}[method]
		if call == "trace" && req.Points != tracePoints || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, `{"code":"invalid_argument"}`, http.StatusBadRequest)
			return
		}
		m, ok := byKey[fmt.Sprintf("%s %s %d", call, req.SessionID, req.Lap)]
		if !ok {
			http.Error(w, `{"code":"not_found"}`, http.StatusNotFound)
			return
		}
		if list, ok := m.(*pitwallv1.ListSessionsResponse); ok && !closed.Load() {
			list = proto.Clone(list).(*pitwallv1.ListSessionsResponse)
			list.Sessions[0].Closed = false
			m = list
		}
		b, _ := protojson.Marshal(m)
		_, _ = w.Write(b)
	}))
}

func newWallV2(t *testing.T, f *fakeRunner, keep bool) (*v2Side, *atomic.Bool, *atomic.Int64) {
	t.Helper()
	var closed atomic.Bool
	var laps atomic.Int64
	pw := fakePitwall(t, &closed)
	t.Cleanup(pw.Close)
	lb := fakeLogbook(&laps)
	t.Cleanup(lb.Close)
	s := newV2(f, lb.URL, keep, &bytes.Buffer{})
	s.pitwall = pw.URL
	s.run = func(ctx context.Context, argv ...string) ([]byte, error) {
		if strings.Contains(strings.Join(argv, " "), "replay replay") {
			laps.Add(4)
			closed.Store(laps.Load() >= 10)
		}
		return f.run(ctx, argv...)
	}
	return s, &closed, &laps
}

func TestV2AsksThePitWallOverTheStack(t *testing.T) {
	f := &fakeRunner{rules: []rule{{has: "config --images", out: "nats\nlogbook:0.1.7\npitwall:0.1.0\n"}}}
	s, _, _ := newWallV2(t, f, false)
	got, err := s.PitWall(context.Background(), wallRecs(), wallClocks(3), 10)
	if err != nil {
		t.Fatal(err)
	}
	v1 := v1Answers(t)
	if r := compareWall(wallRecs(), v1, got); len(r.Diffs) != 0 {
		t.Errorf("%+v", r.Diffs)
	}
	stack := "docker compose -p parity-pitwall -f /paddock/deploy/compose/compose.yaml -f "
	if !strings.HasPrefix(f.calls[0], stack) || !strings.HasSuffix(f.calls[0], "/pitwall.yaml down --volumes --remove-orphans") ||
		!strings.HasSuffix(f.calls[1], " up -d") ||
		!strings.Contains(f.calls[4], "replay replay --src parity --speed max --clock 2026-01-03T00:00:00Z /recording/real-nuc-suzuka-joined-mid-race-2026-10-18.jsonl.gz") ||
		!strings.HasSuffix(f.calls[5], "down --volumes --remove-orphans") || len(f.calls) != 6 {
		t.Errorf("calls\n%s", strings.Join(f.calls, "\n"))
	}
	images, err := s.WallImages(context.Background())
	if err != nil || strings.Join(images, " ") != "nats logbook:0.1.7 pitwall:0.1.0" || !strings.Contains(f.calls[6], "/pitwall.yaml --profile replay config --images") {
		t.Errorf("%q %v %q", images, err, f.calls[6])
	}
}

func TestV2KeepsItsStackAndSettlesOnAnOpenSession(t *testing.T) {
	f := &fakeRunner{}
	s, _, laps := newWallV2(t, f, true)
	s.run = func(ctx context.Context, argv ...string) ([]byte, error) {
		if strings.Contains(strings.Join(argv, " "), "replay replay") {
			laps.Add(4) // never all closed
		}
		return f.run(ctx, argv...)
	}
	if _, err := s.PitWall(context.Background(), wallRecs(), wallClocks(3), 10); err != nil {
		t.Fatal(err)
	}
	if last := f.calls[len(f.calls)-1]; !strings.HasSuffix(last, " stop") {
		t.Errorf("last call %q", last)
	}
}

func TestV2sPitWallFails(t *testing.T) {
	for _, r := range []rule{
		{has: "down", err: errors.New("no compose")},
		{has: "up -d", err: errors.New("no room")},
		{has: "replay replay", err: errors.New("exit 1")},
	} {
		f := &fakeRunner{rules: []rule{r}}
		s, _, _ := newWallV2(t, f, false)
		if _, err := s.PitWall(context.Background(), wallRecs(), wallClocks(3), 10); err == nil {
			t.Errorf("%+v: no error", r)
		}
	}
	f := &fakeRunner{rules: []rule{{has: "config", err: errors.New("bad file")}}}
	s, _, _ := newWallV2(t, f, false)
	if _, err := s.WallImages(context.Background()); err == nil {
		t.Error("images: no error")
	}
	f = &fakeRunner{}
	s, _, _ = newWallV2(t, f, false)
	if _, err := s.WallImages(context.Background()); err == nil {
		t.Error("no images: no error")
	}
	// A pit wall that never hears logbook, or answers an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = io.WriteString(w, `{"logbook":false}`)
			return
		}
		http.Error(w, "nope", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	s, _, _ = newWallV2(t, &fakeRunner{}, false)
	s.pitwall, s.wait = srv.URL, 200*time.Millisecond
	if _, err := s.PitWall(context.Background(), wallRecs(), wallClocks(3), 10); err == nil || !strings.Contains(err.Error(), "logbook false") {
		t.Errorf("%v", err)
	}
	if _, err := s.askPitwall(context.Background()); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("%v", err)
	}
	if err := s.waitClosed(context.Background()); err == nil {
		t.Error("waited for sessions that never came")
	}
}

func TestTheWallOverrideIsHardenedAndPinned(t *testing.T) {
	for _, want := range []string{
		"twr-logbook:0.1.7@sha256:", "twr-pitwall:0.1.0@sha256:", "cap_drop: [ALL]", `"no-new-privileges:true"`,
		"read_only: true", `"127.0.0.1:9852:8080"`, "TWR_PITWALL_SRC: parity",
	} {
		if !strings.Contains(wallOverride, want) {
			t.Errorf("no %q", want)
		}
	}
}
