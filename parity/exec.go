package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runner runs a command and returns its standard output. Its error carries the
// tail of the command's standard error, so a failing container says why. The
// adapters take one, so their tests run without docker.
type runner func(ctx context.Context, argv ...string) ([]byte, error)

// execRunner runs argv on this machine.
func execRunner(ctx context.Context, argv ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // docker and compose, with the harness's own arguments
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %w\n%s", strings.Join(argv, " "), err, tail(stderr.String(), 20))
	}
	return stdout.Bytes(), nil
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
