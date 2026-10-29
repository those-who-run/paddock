package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readRepoSources(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "recordings", "SOURCES.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sums, err := readSources(b)
	if err != nil {
		t.Fatal(err)
	}
	return sums
}

// The harness reads the same index tests/test_recordings.py checks, and R4's three
// are in it.
func TestTheDefaultsAreListed(t *testing.T) {
	sums := readRepoSources(t)
	for _, name := range defaultRecordings {
		if len(sums[name]) != 64 {
			t.Errorf("%s: sha256 %q", name, sums[name])
		}
	}
	recs, err := resolveRecordings(defaultRecordings, filepath.Join("..", "recordings"), sums)
	if err != nil {
		t.Fatal(err)
	}
	if recs[1].Stem() != "real-nuc-suzuka-pit-stop-2026-10-15" || !filepath.IsAbs(recs[1].Path) {
		t.Errorf("%+v", recs[1])
	}
}

func TestANameWithoutItsSuffix(t *testing.T) {
	recs, err := resolveRecordings([]string{"real-race-2laps-2026-10-14"}, filepath.Join("..", "recordings"), readRepoSources(t))
	if err != nil {
		t.Fatal(err)
	}
	if recs[0].Name != "real-race-2laps-2026-10-14.jsonl.gz" {
		t.Errorf("%+v", recs[0])
	}
}

// A recording not in SOURCES.yaml needs a .sha256 beside it.
func TestARecordingOfOnesOwn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20261101-120000.jsonl.gz")
	if err := os.WriteFile(path, []byte("frames"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := resolveRecordings([]string{path}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "no 20261101-120000.jsonl.gz.sha256 beside it") {
		t.Fatalf("no sum: %v", err)
	}
	sum, _ := fileSHA256(path)
	if err := os.WriteFile(path+".sha256", []byte(sum+"  20261101-120000.jsonl.gz\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRecordings([]string{path}, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".sha256", []byte(strings.Repeat("0", 64)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRecordings([]string{path}, "", nil); err == nil || !strings.Contains(err.Error(), "refused: sha256") {
		t.Fatalf("wrong sum: %v", err)
	}
	if err := os.WriteFile(path+".sha256", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRecordings([]string{path}, "", nil); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("empty sum: %v", err)
	}
}

// Every bad recording is named, not only the first.
func TestEveryBadRecordingIsNamed(t *testing.T) {
	_, err := resolveRecordings([]string{"nope-1", "nope-2"}, filepath.Join("..", "recordings"), readRepoSources(t))
	if err == nil || !strings.Contains(err.Error(), "nope-1") || !strings.Contains(err.Error(), "nope-2") {
		t.Fatalf("%v", err)
	}
}

func TestReadSourcesRefusesOtherYAML(t *testing.T) {
	for _, bad := range []string{
		"  - file: a\n",
		"fixtures:\n  - file: [a, b]\n   x\n",
		"fixtures:\n  - file:a\n",
		"fixtures:\n  - v1: a\n",
	} {
		if _, err := readSources([]byte(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestFindRoot(t *testing.T) {
	wd, _ := os.Getwd()
	if root := findRoot(wd); root != filepath.Dir(wd) {
		t.Errorf("root %q, want %q", root, filepath.Dir(wd))
	}
	if root := findRoot(t.TempDir()); root != "" {
		t.Errorf("root %q outside a checkout", root)
	}
}
