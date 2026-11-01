package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// v2Side replays through the compose stack (paddock#39): for each recording a
// fresh project with empty volumes, `ecu replay --src parity --speed max` through
// the stack's `replay` service, then `logbook export --laps --src parity` once
// logbook has stopped (R1, and the decision of 2026-10-24: no --clock).
type v2Side struct {
	run     runner
	compose []string // docker compose, or podman compose
	file    string   // the compose file, absolute
	keep    bool
	log     io.Writer
	// healthz is logbook's /healthz as compose publishes it.
	healthz string
	client  *http.Client
	// wait is how long logbook may take to come up, and then to store the laps;
	// settle is how long its laps_written must stay still to count as done when
	// the other side's lap count isn't reached, and quiet how long once it is.
	wait, settle, quiet, poll time.Duration
	// said is how long radio's spoken lines must stay the same for a spoken run to
	// be over: its replay clock says the last callout a second after it comes.
	said time.Duration
	// nats is NATS as compose publishes it, and listen how the checker listens.
	nats   string
	listen listenFunc
	// pitwall is the pit wall's address, as compose publishes it.
	pitwall string
}

// logbookHealth is the part of logbook's /healthz the harness reads.
type logbookHealth struct {
	Laps int `json:"laps_written"`
}

func (s *v2Side) stack(project string, args ...string) []string {
	return append(append(append([]string{}, s.compose...), "-p", project, "-f", s.file), args...)
}

// Images lists the compose file's images (R5): ecu's, through its replay service,
// logbook's, and NATS's.
func (s *v2Side) Images(ctx context.Context) ([]string, error) {
	// The replay service is in the `replay` profile (#39), and compose lists only
	// the services of active profiles, so ecu would be missing without it.
	out, err := s.run(ctx, append(append([]string{}, s.compose...), "-f", s.file, "--profile", "replay", "config", "--images")...)
	if err != nil {
		return nil, err
	}
	var images []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			images = append(images, line)
		}
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("%s names no images", s.file)
	}
	return images, nil
}

func (s *v2Side) Laps(ctx context.Context, rec Recording, want int) (laps []Lap, err error) {
	project := "parity-" + safeName(rec.Stem())
	if _, err := s.run(ctx, s.stack(project, "down", "--volumes", "--remove-orphans")...); err != nil {
		return nil, err
	}
	defer func() {
		args := []string{"down", "--volumes", "--remove-orphans"}
		if s.keep {
			// Stopped, not removed, so the next recording's stack can take the ports.
			args = []string{"stop"}
			_, _ = fmt.Fprintf(s.log, "parity: kept v2's stack, compose project %s (stopped, volumes kept)\n", project)
		}
		if _, downErr := s.run(context.WithoutCancel(ctx), s.stack(project, args...)...); downErr != nil && err == nil {
			err = downErr
		}
	}()
	if _, err := s.run(ctx, s.stack(project, "up", "-d")...); err != nil {
		return nil, fmt.Errorf("v2 up: %w", err)
	}
	if _, err := s.waitLaps(ctx, -1); err != nil {
		return nil, fmt.Errorf("v2: logbook not healthy: %w", err)
	}
	inside := "/recording/" + rec.Name
	if _, err := s.run(ctx, s.stack(project, "run", "--rm", "--no-deps", "-T",
		"-v", rec.Path+":"+inside+":ro,z",
		"replay", "replay", "--src", "parity", "--speed", "max", inside)...); err != nil {
		return nil, fmt.Errorf("v2 replay: %w", err)
	}
	// ecu has published everything when it exits 0; logbook may still be storing.
	if _, err := s.waitLaps(ctx, want); err != nil {
		return nil, fmt.Errorf("v2: waiting for logbook's laps: %w", err)
	}
	// logbook export takes the store's lock, so logbook is stopped first (#39).
	if _, err := s.run(ctx, s.stack(project, "stop", "logbook")...); err != nil {
		return nil, err
	}
	out, err := s.run(ctx, s.stack(project, "run", "--rm", "--no-deps", "-T",
		"logbook", "export", "--data", "/data", "--laps", "--src", "parity")...)
	if err != nil {
		return nil, fmt.Errorf("v2 export: %w", err)
	}
	stored, err := readStoredLaps(out)
	if err != nil {
		return nil, fmt.Errorf("v2 export: %w", err)
	}
	return normalise(stored), nil
}

// waitLaps polls logbook's /healthz. With want < 0 it returns once logbook answers
// 200. Otherwise it returns once laps_written has reached want and stayed the same
// for quiet, or, when want is 0 or isn't reached, has stayed the same for settle: a
// v2 that stores fewer laps than v1 is a difference to report, not a wait without
// end. Reaching want isn't enough on its own: v2 may store a lap more than v1 (an
// incomplete last lap, say), and stopping logbook early would hand that lap's
// events back unstored and hide the difference.
func (s *v2Side) waitLaps(ctx context.Context, want int) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	last, since := -1, time.Now()
	for {
		laps, err := s.health(ctx)
		switch {
		case err != nil:
		case want < 0:
			return laps, nil
		case laps != last:
			last, since = laps, time.Now()
		case want > 0 && laps >= want && time.Since(since) >= s.quiet:
			return laps, nil
		case time.Since(since) >= s.settle:
			return laps, nil
		}
		select {
		case <-ctx.Done():
			if err == nil {
				err = ctx.Err()
			}
			return 0, err
		case <-time.After(s.poll):
		}
	}
}

func (s *v2Side) health(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.healthz, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.client.Do(req) //nolint:gosec // logbook's /healthz on loopback, the harness's own URL
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, errors.New(resp.Status)
	}
	var h logbookHealth
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return 0, err
	}
	return h.Laps, nil
}

// eventSubjects are what the checker listens to (R1): every event and callout
// under the harness's source.
var eventSubjects = []string{"twr.v1.parity.evt.>", "twr.v1.parity.callout.>"}

// replayClock is the wall-clock time `ecu replay` gives a recording's first frame
// when it is passed no --clock (ecu's README, "Replay"), as v1Clock is v1's.
var replayClock = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// restartGap is how long the engineer was stopped between two recordings of a
// run, from one's last frame to the next's first, on both sides: the gap ecu's
// golden for the restart pair has (ecu#56), well inside the continue window.
const restartGap = 8700 * time.Millisecond

// replayArgs are ecu replay's arguments for each recording of a run (#76, ecu#56's
// "How paddock should run the restart pair"): every one under the one --src; each
// but the last with --keep-open, so it stops as an ecu does and keeps its session
// in the `ecu` bucket; each after the first with --clock at restartGap after the
// last frame of the one before, which it then continues. A recording on its own
// gets no --clock (the decision of 2026-10-24).
func replayArgs(run Run) ([][]string, error) {
	var out [][]string
	clock := replayClock
	for i, rec := range run.Recordings {
		args := []string{"replay", "replay", "--src", "parity", "--speed", "max"}
		if i > 0 {
			args = append(args, "--clock", clock.Format(time.RFC3339Nano))
		}
		if i < len(run.Recordings)-1 {
			args = append(args, "--keep-open")
			ts, err := frameTimes(rec.Path)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", rec.Name, err)
			}
			span := time.Duration(math.Round((ts[len(ts)-1] - ts[0]) * float64(time.Second)))
			clock = clock.Add(span + restartGap)
		}
		out = append(out, append(args, "/recording/"+rec.Name))
	}
	return out, nil
}

// Events replays the run's recordings, in order, through a fresh stack, with the
// checker listening on the bus from before the first frame (R1, R6): ecu's
// decisions as it publishes them, not as a store keeps them. Each recording is a
// part: ecu's frame count starts again with each replay. A run of several is one
// engineer stopped and started again (replayArgs). The stack is the run's own, so
// no kept session of another run's is in its `ecu` bucket.
func (s *v2Side) Events(ctx context.Context, run Run) (items []Item, err error) {
	replays, err := replayArgs(run)
	if err != nil {
		return nil, fmt.Errorf("v2: %w", err)
	}
	project := "parity-" + safeName(run.Stem())
	if _, err := s.run(ctx, s.stack(project, "down", "--volumes", "--remove-orphans")...); err != nil {
		return nil, err
	}
	defer func() {
		args := []string{"down", "--volumes", "--remove-orphans"}
		if s.keep {
			args = []string{"stop"}
			_, _ = fmt.Fprintf(s.log, "parity: kept v2's stack, compose project %s (stopped, volumes kept)\n", project)
		}
		if _, downErr := s.run(context.WithoutCancel(ctx), s.stack(project, args...)...); downErr != nil && err == nil {
			err = downErr
		}
	}()
	if _, err := s.run(ctx, s.stack(project, "up", "-d")...); err != nil {
		return nil, fmt.Errorf("v2 up: %w", err)
	}
	// logbook answers once the bootstrap has made the streams: ecu's publishes
	// need them.
	if _, err := s.waitLaps(ctx, -1); err != nil {
		return nil, fmt.Errorf("v2: logbook not healthy: %w", err)
	}
	l, err := s.listen(s.nats, eventSubjects...)
	if err != nil {
		return nil, fmt.Errorf("v2: listening on %s: %w", s.nats, err)
	}
	defer l.Close()
	sessions, seen := map[string]int{}, map[string]bool{}
	heard := 0
	for i, rec := range run.Recordings {
		inside := "/recording/" + rec.Name
		if _, err := s.run(ctx, s.stack(project, append([]string{"run", "--rm", "--no-deps", "-T",
			"-v", rec.Path + ":" + inside + ":ro,z"}, replays[i]...)...)...); err != nil {
			return nil, fmt.Errorf("v2 replay: %w", err)
		}
		msgs, err := s.waitQuiet(ctx, l)
		if err != nil {
			return nil, fmt.Errorf("v2: listening: %w", err)
		}
		part, err := v2Items(msgs[heard:], i+1, sessions, seen)
		if err != nil {
			return nil, fmt.Errorf("v2: %w", err)
		}
		heard = len(msgs)
		items = append(items, part...)
	}
	return items, nil
}

// waitQuiet returns what the checker has heard once nothing new has come for
// quiet. ecu has published everything when its replay exits, so this is only the
// last messages' way to the checker.
func (s *v2Side) waitQuiet(ctx context.Context, l listener) ([]busMsg, error) {
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	last, since := -1, time.Now()
	for {
		msgs := l.Taken()
		if len(msgs) != last {
			last, since = len(msgs), time.Now()
		} else if time.Since(since) >= s.quiet {
			return msgs, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(s.poll):
		}
	}
}
