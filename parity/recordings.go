package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// defaultRecordings are R4's three: a clean race, a race with a pit stop and
// restarts, from recordings/ at the repository's root.
var defaultRecordings = []string{
	"real-nuc-suzuka-no-stop-2026-10-15.jsonl.gz",
	"real-nuc-suzuka-pit-stop-2026-10-15.jsonl.gz",
	"real-nuc-sugo-restarts-2026-10-18.jsonl.gz",
}

// Recording is a recording to replay on both sides, its sha256 checked.
type Recording struct {
	Name   string // the file's base name, which both sides see it as
	Path   string // where it is, absolute
	SHA256 string
}

// Stem is the recording's name without .jsonl or .jsonl.gz.
func (r Recording) Stem() string {
	return strings.TrimSuffix(strings.TrimSuffix(r.Name, ".gz"), ".jsonl")
}

// readSources reads recordings/SOURCES.yaml into each file's sha256, by file name.
// The file is the subset of YAML it says it is (a list of `key: value` entries
// under `fixtures:`), the same subset tests/test_recordings.py reads; anything else
// is an error rather than a guess.
func readSources(b []byte) (map[string]string, error) {
	sums := map[string]string{}
	var entry map[string]string
	done := func() error {
		if entry == nil {
			return nil
		}
		if entry["file"] == "" || entry["sha256"] == "" {
			return fmt.Errorf("SOURCES.yaml: an entry without file or sha256: %v", entry)
		}
		sums[entry["file"]] = entry["sha256"]
		return nil
	}
	inList := false
	for n, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		where := fmt.Sprintf("SOURCES.yaml:%d", n+1)
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case line == "fixtures:" && !inList:
			inList = true
			continue
		case !inList:
			return nil, fmt.Errorf("%s: %q before `fixtures:`", where, line)
		}
		var item string
		switch {
		case strings.HasPrefix(line, "  - "):
			if err := done(); err != nil {
				return nil, err
			}
			entry = map[string]string{}
			item = strings.TrimPrefix(line, "  - ")
		case strings.HasPrefix(line, "    ") && entry != nil:
			item = strings.TrimPrefix(line, "    ")
		default:
			return nil, fmt.Errorf("%s: %q is not an entry's line", where, line)
		}
		key, value, ok := strings.Cut(item, ": ")
		if !ok || strings.ContainsAny(key, " :") || value == "" {
			return nil, fmt.Errorf("%s: %q is not `key: value`", where, line)
		}
		entry[key] = value
	}
	if err := done(); err != nil {
		return nil, err
	}
	return sums, nil
}

// resolveRecordings finds and checks every recording before anything runs (AC3).
// An argument is a path to a file, or the name of a file in dir (with or without
// .jsonl.gz). Its sha256 must be the one SOURCES.yaml lists for its file name, or,
// for a file not listed there, the one in a `<path>.sha256` file beside it
// (sha256sum's format). A recording with neither, or whose sum differs, is refused.
func resolveRecordings(args []string, dir string, sums map[string]string) ([]Recording, error) {
	var recs []Recording
	var errs []error
	for _, arg := range args {
		rec, err := resolveRecording(arg, dir, sums)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		recs = append(recs, rec)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return recs, nil
}

func resolveRecording(arg, dir string, sums map[string]string) (Recording, error) {
	path := arg
	if _, err := os.Stat(path); err != nil && dir != "" && !strings.ContainsRune(arg, filepath.Separator) {
		name := arg
		if !strings.HasSuffix(name, ".jsonl.gz") && !strings.HasSuffix(name, ".jsonl") {
			name += ".jsonl.gz"
		}
		path = filepath.Join(dir, name)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return Recording{}, fmt.Errorf("%s: %w", arg, err)
	}
	got, err := fileSHA256(abs)
	if err != nil {
		return Recording{}, fmt.Errorf("%s: %w", arg, err)
	}
	name := filepath.Base(abs)
	want, where := sums[name], "recordings/SOURCES.yaml"
	if want == "" {
		where = abs + ".sha256"
		b, err := os.ReadFile(where) //nolint:gosec // a sum file beside a recording the caller named
		if err != nil {
			return Recording{}, fmt.Errorf("%s: refused: not in recordings/SOURCES.yaml and no %s beside it", arg, filepath.Base(where))
		}
		fields := strings.Fields(string(b))
		if len(fields) == 0 {
			return Recording{}, fmt.Errorf("%s: refused: %s is empty", arg, where)
		}
		want = fields[0]
	}
	if !strings.EqualFold(got, want) {
		return Recording{}, fmt.Errorf("%s: refused: sha256 %s, %s says %s", arg, got, where, want)
	}
	return Recording{Name: name, Path: abs, SHA256: got}, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // a recording the caller named
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// findRoot is the paddock checkout around dir: the nearest directory, dir or above,
// with recordings/SOURCES.yaml. "" when there is none.
func findRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "recordings", "SOURCES.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
