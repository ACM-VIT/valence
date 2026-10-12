package problems

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every example package in the repo must stay valid.
func TestLoadExamples(t *testing.T) {
	dirs, _ := filepath.Glob("../../../../problems/examples/*")
	if len(dirs) == 0 {
		t.Fatal("no example packages found")
	}
	for _, dir := range dirs {
		if _, err := Load(dir); err != nil {
			t.Errorf("%s:\n%v", dir, err)
		}
	}
}

func TestLoadAplusb(t *testing.T) {
	p, err := Load("../../../../problems/examples/aplusb")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "aplusb" || p.MemoryLimitKiB != 256*1024 || p.Checker != "tokens" {
		t.Errorf("got slug %q, memory %d KiB, checker %q", p.Slug, p.MemoryLimitKiB, p.Checker)
	}
	if len(p.Tests) != 5 {
		t.Fatalf("got %d tests, want 5", len(p.Tests))
	}
	if !p.Tests[0].Sample || !p.Tests[1].Sample || p.Tests[2].Sample {
		t.Error("tests 1 and 2 should be samples, test 3 should not")
	}
}

// A package with many mistakes reports all of them, not just the first.
func TestLoadReportsEveryError(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("problem.yaml", "version: 0\nslug: Bad_Slug\ntitle: X\ntime_limit: 2000\nsamples: [5]\n")
	write("statement.md", "hi \xff")
	write("tests/01.in", "1")
	write("tests/01.out", "1")
	write("tests/03.in", "3")
	write("tests/notes.txt", "")

	_, err := Load(dir)
	if err == nil {
		t.Fatal("invalid package was accepted")
	}
	for _, want := range []string{"time_limit", "slug", "test 2 is missing", "test 3 has an .in but no .out", "notes.txt", "sample 5", "not valid UTF-8"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %q:\n%v", want, err)
		}
	}
}
