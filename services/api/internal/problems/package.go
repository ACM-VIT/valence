package problems

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	maxSampleBytes    = 64 << 10
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
		} else if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
			// A second document would otherwise be ignored, typos and all.
			fail("problem.yaml: must contain exactly one YAML document")
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

	statement, err := readAtMost(filepath.Join(dir, "statement.md"), maxStatementBytes)
	if err != nil {
		fail("statement.md: %v", err)
	} else if len(statement) > maxStatementBytes {
		fail("statement.md: larger than the limit of %d bytes", maxStatementBytes)
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
			for _, f := range []string{tests[s-1].In, tests[s-1].Out} {
				if info, err := os.Stat(f); err == nil && info.Size() > maxSampleBytes {
					fail("tests/%s: samples should be at most 64 KiB", filepath.Base(f))
				}
			}
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
		// Only plain files: a symlink could point at /dev/zero or outside
		// the package, and its reported size is the link's, not the target's.
		if match == nil || !e.Type().IsRegular() {
			fail("tests/%s: only regular NN.in and NN.out files belong here", name)
			continue
		}
		if width == 0 {
			width = len(match[1])
		} else if len(match[1]) != width {
			fail("tests/%s: use the same number of digits for every test", name)
		}
		info, err := e.Info()
		if err != nil {
			fail("tests/%s: %v", name, err)
			continue
		}
		if info.Size() > maxTestFileBytes {
			fail("tests/%s: larger than 64 MiB", name)
		} else if msg := checkTestText(filepath.Join(dir, name)); msg != "" {
			fail("tests/%s: %s", name, msg)
		}

		n, err := strconv.Atoi(match[1])
		if err != nil || n < 1 {
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
	// Walk only the numbers that exist, so a file named 1000000000.in can't
	// make us allocate or loop over a billion missing tests.
	nums := make([]int, 0, len(files))
	for n := range files {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	tests := make([]TestFiles, 0, len(nums))
	gap := false
	for i, n := range nums {
		if n != i+1 && !gap {
			fail("tests: test %d is missing; number tests from 1 with no gaps", i+1)
			gap = true
		}
		t := files[n]
		switch {
		case t.In == "":
			fail("tests: test %d has an .out but no .in", n)
		case t.Out == "":
			fail("tests: test %d has an .in but no .out", n)
		}
		tests = append(tests, *t)
	}
	return tests
}

// checkTestText applies the format's rules for test files: UTF-8, \n line
// endings and a final newline. Samples go out as protobuf strings, which
// must be valid UTF-8. It returns "" when the file is fine.
func checkTestText(path string) string {
	b, err := os.ReadFile(path)
	switch {
	case err != nil:
		return err.Error()
	case len(b) == 0:
		return ""
	case !utf8.Valid(b):
		return "not valid UTF-8"
	case bytes.IndexByte(b, '\r') >= 0:
		return `use \n line endings, not \r\n`
	case b[len(b)-1] != '\n':
		return "must end with a newline"
	}
	return ""
}

// readAtMost reads up to limit+1 bytes of path, so an oversized file is
// detected without loading all of it.
func readAtMost(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}
