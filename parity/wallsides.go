package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	pitwallv1 "github.com/those-who-run/contracts/gen/go/twr/pitwall/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// defaultPitwall is the pit wall as compose publishes it: v1's port (#124's
// triage, #132).
const defaultPitwall = "http://127.0.0.1:9852"

// wallOverride is the compose file laid over the stack's for a pit wall run: only
// the pit wall's source, so the run checks compose's own pit wall and logbook,
// the images Renovate bumps.
const wallOverride = `services:
  pitwall:
    environment:
      TWR_PITWALL_SRC: parity
`

// v1PitwallScript is v1pitwall.py: v1's replays, then v1's pit wall asked.
//
//go:embed v1pitwall.py
var v1PitwallScript string

func (s *v1Side) WallImages(ctx context.Context) ([]string, error) {
	return s.Images(ctx)
}

// PitWall replays each recording with v1's own `replay` onto one store, each on
// its clock, and asks v1's pit wall app over it, in v1's own image.
func (s *v1Side) PitWall(ctx context.Context, recs []Recording, clocks []time.Time, _ int) ([]wallAnswer, error) {
	args := []string{s.container, "run", "--rm"}
	var inside []string
	for _, rec := range recs {
		path := "/recording/" + rec.Name
		args = append(args, "-v", rec.Path+":"+path+":ro,z")
		inside = append(inside, path)
	}
	args = append(args, "--entrypoint", "python", s.image, "-c", v1PitwallScript)
	for _, c := range clocks {
		args = append(args, "--clock", c.Format(time.RFC3339Nano))
	}
	out, err := s.run(ctx, append(args, inside...)...)
	if err != nil {
		return nil, fmt.Errorf("v1 pitwall: %w", err)
	}
	answers, err := readWallAnswers(out)
	if err != nil {
		return nil, fmt.Errorf("v1 pitwall: %w", err)
	}
	return answers, nil
}

// wallStack is the compose command for the pit wall's project, with the override.
func (s *v2Side) wallStack(project, override string, args ...string) []string {
	return append(append(append([]string{}, s.compose...), "-p", project, "-f", s.file, "-f", override), args...)
}

// WallImages lists the images of the stack with its replay profile, the pit wall's
// among them.
func (s *v2Side) WallImages(ctx context.Context) ([]string, error) {
	dir, override, err := writeOverride()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out, err := s.run(ctx, append(append([]string{}, s.compose...), "-f", s.file, "-f", override, "--profile", "replay", "config", "--images")...)
	if err != nil {
		return nil, err
	}
	images := strings.Fields(string(out))
	if len(images) == 0 {
		return nil, fmt.Errorf("%s names no images", s.file)
	}
	return images, nil
}

func writeOverride() (dir, file string, err error) {
	dir, err = os.MkdirTemp("", "parity-pitwall-compose-")
	if err != nil {
		return "", "", err
	}
	file = filepath.Join(dir, "pitwall.yaml")
	if err := os.WriteFile(file, []byte(wallOverride), 0o644); err != nil { //nolint:gosec // read by compose, not a secret
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return dir, file, nil
}

// pitwallHealth is the part of the pit wall's /healthz the harness reads.
type pitwallHealth struct {
	Logbook bool `json:"logbook"`
}

// PitWall replays the recordings, in order, through a fresh stack with the pit
// wall over logbook (R1): `ecu replay --src parity --speed max --clock T` for
// each, then, once logbook has stored the laps and the pit wall lists every
// session closed, the same questions v1pitwall.py asks v1, of the pit wall's
// Connect API.
func (s *v2Side) PitWall(ctx context.Context, recs []Recording, clocks []time.Time, want int) (answers []wallAnswer, err error) {
	dir, override, err := writeOverride()
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	const project = "parity-pitwall"
	// By profile, so the pit wall goes down with the rest.
	down := []string{"--profile", "replay", "down", "--volumes", "--remove-orphans"}
	if _, err := s.run(ctx, s.wallStack(project, override, down...)...); err != nil {
		return nil, err
	}
	defer func() {
		args := down
		if s.keep {
			args = []string{"--profile", "replay", "stop"}
			_, _ = fmt.Fprintf(s.log, "parity: kept v2's stack, compose project %s (stopped, volumes kept)\n", project)
		}
		if _, downErr := s.run(context.WithoutCancel(ctx), s.wallStack(project, override, args...)...); downErr != nil && err == nil {
			err = downErr
		}
	}()
	if _, err := s.run(ctx, s.wallStack(project, override, "up", "-d")...); err != nil {
		return nil, fmt.Errorf("v2 up: %w", err)
	}
	// The pit wall is in compose's replay profile, with radio and podium, which the
	// run doesn't want; by profile and by name, as spoken.go starts radio, since
	// podman compose doesn't enable a profile for a service named on the command
	// line.
	if _, err := s.run(ctx, s.wallStack(project, override, "--profile", "replay", "up", "-d", "--no-deps", "pitwall")...); err != nil {
		return nil, fmt.Errorf("v2 up: %w", err)
	}
	if _, err := s.waitLaps(ctx, -1); err != nil {
		return nil, fmt.Errorf("v2: logbook not healthy: %w", err)
	}
	if err := s.waitPitwall(ctx); err != nil {
		return nil, fmt.Errorf("v2: the pit wall doesn't see logbook: %w", err)
	}
	for i, rec := range recs {
		inside := "/recording/" + rec.Name
		if _, err := s.run(ctx, s.wallStack(project, override, "run", "--rm", "--no-deps", "-T",
			"-v", rec.Path+":"+inside+":ro,z",
			"replay", "replay", "--src", "parity", "--speed", "max",
			"--clock", clocks[i].Format(time.RFC3339Nano), inside)...); err != nil {
			return nil, fmt.Errorf("v2 replay: %w", err)
		}
	}
	if _, err := s.waitLaps(ctx, want); err != nil {
		return nil, fmt.Errorf("v2: waiting for logbook's laps: %w", err)
	}
	if err := s.waitClosed(ctx); err != nil {
		return nil, fmt.Errorf("v2: waiting for the sessions to close: %w", err)
	}
	return s.askPitwall(ctx)
}

// waitPitwall polls the pit wall's /healthz until it has heard logbook answer.
func (s *v2Side) waitPitwall(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	for {
		var h pitwallHealth
		err := s.getJSON(ctx, s.pitwall+"/healthz", &h)
		if err == nil && h.Logbook {
			return nil
		}
		select {
		case <-ctx.Done():
			if err == nil {
				err = errors.New("logbook false")
			}
			return fmt.Errorf("%w: %w", ctx.Err(), err)
		case <-time.After(s.poll):
		}
	}
}

// waitClosed polls the session list until every session in it is closed, or it
// has stayed the same for settle: ecu publishes a session's end when its replay
// ends, and logbook may still be storing it.
func (s *v2Side) waitClosed(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	last, since := "", time.Now()
	for {
		resp := &pitwallv1.ListSessionsResponse{}
		if err := s.connect(ctx, "ListSessions", &pitwallv1.ListSessionsRequest{}, resp); err == nil {
			open := 0
			for _, x := range resp.GetSessions() {
				if !x.GetClosed() {
					open++
				}
			}
			b, _ := protojson.Marshal(resp)
			switch {
			case len(resp.GetSessions()) > 0 && open == 0:
				return nil
			case string(b) != last:
				last, since = string(b), time.Now()
			case time.Since(since) >= s.settle:
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.poll):
		}
	}
}

// askPitwall asks what v1pitwall.py asks: the list, then each session, its laps,
// its same-track sessions and each complete lap's trace at tracePoints.
func (s *v2Side) askPitwall(ctx context.Context) ([]wallAnswer, error) {
	var out []wallAnswer
	ask := func(call, method, id string, lap int, req, resp proto.Message) error {
		if err := s.connect(ctx, method, req, resp); err != nil {
			return fmt.Errorf("%s %s: %w", method, protojson.Format(req), err)
		}
		body, err := v1Shape(resp)
		if err != nil {
			return err
		}
		out = append(out, wallAnswer{Call: call, SessionID: id, Lap: lap, Body: body})
		return nil
	}
	list := &pitwallv1.ListSessionsResponse{}
	if err := ask("sessions", "ListSessions", "", 0, &pitwallv1.ListSessionsRequest{}, list); err != nil {
		return nil, err
	}
	for _, x := range list.GetSessions() {
		id := x.GetSessionId()
		if err := ask("session", "GetSession", id, 0, &pitwallv1.GetSessionRequest{SessionId: id}, &pitwallv1.GetSessionResponse{}); err != nil {
			return nil, err
		}
		laps := &pitwallv1.ListLapsResponse{}
		if err := ask("laps", "ListLaps", id, 0, &pitwallv1.ListLapsRequest{SessionId: id}, laps); err != nil {
			return nil, err
		}
		if err := ask("same_track", "ListSameTrack", id, 0, &pitwallv1.ListSameTrackRequest{SessionId: id}, &pitwallv1.ListSameTrackResponse{}); err != nil {
			return nil, err
		}
		for _, l := range laps.GetLaps() {
			if !l.GetComplete() {
				continue
			}
			req := &pitwallv1.GetTraceRequest{SessionId: id, Lap: l.GetLap(), Points: tracePoints}
			if err := ask("trace", "GetTrace", id, int(l.GetLap()), req, &pitwallv1.GetTraceResponse{}); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// connect calls one of PitwallService's methods with Connect's unary protocol
// in JSON: a POST of the request, the response's JSON back.
func (s *v2Side) connect(ctx context.Context, method string, req, resp proto.Message) error {
	body, err := protojson.Marshal(req)
	if err != nil {
		return err
	}
	url := s.pitwall + "/twr.pitwall.v1.PitwallService/" + method
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(r) //nolint:gosec // the pit wall on loopback, the harness's own URL
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(b)))
	}
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(b, resp)
}

func (s *v2Side) getJSON(ctx context.Context, url string, v any) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := s.client.Do(r) //nolint:gosec // the pit wall on loopback, the harness's own URL
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return errors.New(res.Status)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// v1Shape is a twr.pitwall.v1 response in v1's names and shapes (R3), by
// contracts' own mapping of v1's schema (contracts' pitwall_v1_test.go): every
// field, null where it is absent; a session or a trace unwrapped from its
// response, as v1's body was the thing itself; Timestamps as Unix seconds; v1's
// names for the renamed fields; total_laps 0 where it is absent, as v1 said; and
// the fields v1 had no value for left out.
func v1Shape(resp proto.Message) (map[string]any, error) {
	b, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}.Marshal(resp)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, err
	}
	switch resp.(type) {
	case *pitwallv1.GetSessionResponse:
		body, _ = body["session"].(map[string]any)
	case *pitwallv1.GetTraceResponse:
		body, _ = body["trace"].(map[string]any)
	}
	if body == nil {
		return nil, fmt.Errorf("%T: no body", resp)
	}
	switch resp.(type) {
	case *pitwallv1.GetSessionResponse:
		if err := v1Session(body); err != nil {
			return nil, err
		}
	case *pitwallv1.ListSessionsResponse, *pitwallv1.ListSameTrackResponse:
		sessions, _ := body["sessions"].([]any)
		for _, x := range sessions {
			if err := v1Session(x.(map[string]any)); err != nil {
				return nil, err
			}
		}
	}
	return body, nil
}

// v1Renamed are the session's fields contracts renamed, by their name here, with
// v1's.
var v1Renamed = map[string]string{"best_lap": "best_lap_no", "fuel_burned": "fuel_used"}

// v2Only are the session's fields v1 had no value for (contracts' pitwallNew).
var v2Only = []string{"card_posted_at", "from_v1_rows"}

func v1Session(s map[string]any) error {
	for from, to := range v1Renamed {
		s[to] = s[from]
		delete(s, from)
	}
	for _, k := range v2Only {
		delete(s, k)
	}
	if t, ok := s["track"].(map[string]any); ok {
		t["id"] = t["track_id"]
		delete(t, "track_id")
	}
	if s["total_laps"] == nil {
		s["total_laps"] = 0.0
	}
	for _, k := range []string{"started_at", "ended_at"} {
		v, ok := s[k].(string)
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		s[k] = float64(t.UnixNano()) / 1e9
	}
	return nil
}
