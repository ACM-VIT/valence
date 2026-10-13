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

func TestLoadRejectsHugeSamples(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../../../problems/examples/aplusb")); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("1 ", 40<<10) // 80 KiB
	if err := os.WriteFile(filepath.Join(dir, "tests", "01.in"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("got %v, want an error about the 64 KiB sample limit", err)
	}
}

// copyAplusb returns a writable copy of the aplusb example to break on purpose.
func copyAplusb(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../../../../problems/examples/aplusb")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func wantLoadError(t *testing.T, dir string, wants ...string) {
	t.Helper()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("invalid package was accepted")
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %q:\n%v", want, err)
		}
	}
}

// A huge test number must be reported as a gap, not allocated for.
func TestLoadHugeTestNumber(t *testing.T) {
	dir := copyAplusb(t)
	for _, name := range []string{"1000000000.in", "1000000000.out"} {
		if err := os.WriteFile(filepath.Join(dir, "tests", name), []byte("1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wantLoadError(t, dir, "test 6 is missing", "same number of digits")
}

func TestLoadRejectsSymlinkedTests(t *testing.T) {
	dir := copyAplusb(t)
	in := filepath.Join(dir, "tests", "03.in")
	os.Remove(in)
	if err := os.Symlink("/dev/zero", in); err != nil {
		t.Skip("symlinks not supported:", err)
	}
	wantLoadError(t, dir, "tests/03.in: only regular")
}

func TestLoadChecksTestText(t *testing.T) {
	dir := copyAplusb(t)
	for name, content := range map[string]string{
		"03.in":  "1 \xff\n",    // not UTF-8
		"04.in":  "1 2\r\n",     // CRLF
		"05.out": "-2000000000", // no final newline
	} {
		if err := os.WriteFile(filepath.Join(dir, "tests", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wantLoadError(t, dir, "tests/03.in: not valid UTF-8", `tests/04.in: use \n line endings`, "tests/05.out: must end with a newline")
}

func TestLoadRejectsSecondYAMLDocument(t *testing.T) {
	dir := copyAplusb(t)
	path := filepath.Join(dir, "problem.yaml")
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(raw, "---\ntime_limt_ms: 5\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	wantLoadError(t, dir, "exactly one YAML document")
}

// A huge statement is rejected after reading only a little more than the limit.
func TestLoadRejectsHugeStatement(t *testing.T) {
	dir := copyAplusb(t)
	f, err := os.Create(filepath.Join(dir, "statement.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1 << 30); err != nil { // sparse 1 GiB
		t.Fatal(err)
	}
	f.Close()
	wantLoadError(t, dir, "statement.md: larger than the limit")
}
