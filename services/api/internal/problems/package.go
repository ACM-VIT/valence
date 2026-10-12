package problems

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Package is a problem package that passed validation (docs/problem-format.md).
type Package struct {
	Slug           string
	Title          string
	Statement      string
	TimeLimitMS    int32
	MemoryLimitKiB int32
	Checker        string
	Tests          []TestFiles
}

// TestFiles points at one test's input and expected output on disk.
type TestFiles struct {
	Ordinal int32
	In, Out string
	Sample  bool
}

// manifest mirrors problem.yaml.
type manifest struct {
	Version       int    `yaml:"version"`
	Slug          string `yaml:"slug"`
	Title         string `yaml:"title"`
	TimeLimitMS   int    `yaml:"time_limit_ms"`
	MemoryLimitMB int    `yaml:"memory_limit_mb"`
	Checker       string `yaml:"checker"`
	Samples       []int  `yaml:"samples"`
}

const (
	maxStatementBytes = 256 << 10
	maxTestFileBytes  = 64 << 20
)

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)
	checkerPattern  = regexp.MustCompile(`^(exact|tokens|float:[0-9]+(\.[0-9]+)?([eE][-+]?[0-9]+)?)$`)
	testFilePattern = regexp.MustCompile(`^([0-9]{2,})\.(in|out)$`)
)

// Load reads and validates the package in dir. It doesn't stop at the
// first mistake: the error lists every problem it found.
func Load(dir string) (*Package, error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	// Defaults for optional keys; decoding only overwrites keys that are present.
	m := manifest{Version: -1, TimeLimitMS: 1000, MemoryLimitMB: 256, Checker: "tokens"}
	raw, err := os.ReadFile(filepath.Join(dir, "problem.yaml"))
	if err != nil {
		fail("problem.yaml: %v", err)
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true) // unknown keys are errors
		if err := dec.Decode(&m); err != nil {
			fail("problem.yaml: %v", err)
		}
		if m.Version != 0 {
			fail("problem.yaml: version must be 0")
		}
		if !slugPattern.MatchString(m.Slug) {
			fail("problem.yaml: slug %q must match %s", m.Slug, slugPattern)
		}
		if n := utf8.RuneCountInString(m.Title); n < 1 || n > 200 {
			fail("problem.yaml: title must be 1 to 200 characters")
		}
		if m.TimeLimitMS < 100 || m.TimeLimitMS > 20000 {
			fail("problem.yaml: time_limit_ms must be 100 to 20000")
		}
		if m.MemoryLimitMB < 16 || m.MemoryLimitMB > 2048 {
			fail("problem.yaml: memory_limit_mb must be 16 to 2048")
		}
		if !checkerPattern.MatchString(m.Checker) {
			fail("problem.yaml: checker %q must be exact, tokens or float:EPS", m.Checker)
		}
	}

	statement, err := os.ReadFile(filepath.Join(dir, "statement.md"))
	if err != nil {
		fail("statement.md: %v", err)
	} else if len(statement) > maxStatementBytes {
		fail("statement.md: %d bytes, the limit is %d", len(statement), maxStatementBytes)
	} else if !utf8.Valid(statement) {
		fail("statement.md: not valid UTF-8")
	}

	tests := loadTests(filepath.Join(dir, "tests"), fail)

	seen := map[int]bool{}
	for _, s := range m.Samples {
		switch {
		case s < 1 || s > len(tests):
			fail("problem.yaml: sample %d is not a test", s)
		case seen[s]:
			fail("problem.yaml: sample %d is listed twice", s)
		default:
			seen[s] = true
			tests[s-1].Sample = true
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &Package{
		Slug:           m.Slug,
		Title:          m.Title,
		Statement:      string(statement),
		TimeLimitMS:    int32(m.TimeLimitMS),
		MemoryLimitKiB: int32(m.MemoryLimitMB * 1024),
		Checker:        m.Checker,
		Tests:          tests,
	}, nil
}

// loadTests pairs up tests/NN.in and tests/NN.out, numbered from 1 with no gaps.
func loadTests(dir string, fail func(string, ...any)) []TestFiles {
	entries, err := os.ReadDir(dir)
	if err != nil {
		fail("tests: %v", err)
		return nil
	}

	files := map[int]*TestFiles{}
	width := 0
	for _, e := range entries {
		name := e.Name()
		match := testFilePattern.FindStringSubmatch(name)
		if e.IsDir() || match == nil {
			fail("tests/%s: only NN.in and NN.out files belong here", name)
			continue
		}
		if width == 0 {
			width = len(match[1])
		} else if len(match[1]) != width {
			fail("tests/%s: use the same number of digits for every test", name)
		}
		if info, err := e.Info(); err == nil && info.Size() > maxTestFileBytes {
			fail("tests/%s: larger than 64 MiB", name)
		}

		n, _ := strconv.Atoi(match[1])
		if n == 0 {
			fail("tests/%s: tests are numbered from 01", name)
			continue
		}
		if files[n] == nil {
			files[n] = &TestFiles{Ordinal: int32(n)}
		}
		path := filepath.Join(dir, name)
		if match[2] == "in" {
			files[n].In = path
		} else {
			files[n].Out = path
		}
	}

	if len(files) == 0 {
		fail("tests: at least one test is required")
		return nil
	}
	last := 0
	for n := range files {
		last = max(last, n)
	}
	tests := make([]TestFiles, 0, last)
	for i := 1; i <= last; i++ {
		t, ok := files[i]
		switch {
		case !ok:
			fail("tests: test %d is missing; number tests from 1 with no gaps", i)
			continue
		case t.In == "":
			fail("tests: test %d has an .out but no .in", i)
		case t.Out == "":
			fail("tests: test %d has an .in but no .out", i)
		}
		tests = append(tests, *t)
	}
	return tests
}
