package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
