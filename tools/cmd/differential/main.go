// Command differential runs the workspace test suite under three build
// variants -- the default (scalar) toolchain, GOEXPERIMENT=simd, and
// GOEXPERIMENT=simd with GODEC_FORCE_SCALAR=1 (forcing every dsp.HasAVX2/
// HasAVX2FMA-gated dispatch in a SIMD-capable build onto its scalar path,
// see sdk/dsp/cpu_simd.go) -- and prints a single joined report comparing
// them, instead of separate pass/fail runs a reader has to compare by
// hand.
//
// docs/refactor/checkpoint.md M0#4/M0-R1: a shared failure (the same
// package failing identically under every variant) must not exit 0, a
// package pattern that resolves to nothing must not exit 0, and the
// scalar variant must not silently inherit a SIMD-enabling GOEXPERIMENT
// from the calling shell.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "differential:", err)
		os.Exit(1)
	}
	if err := run(cwd, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "differential:", err)
		os.Exit(1)
	}
}

// run is the whole program below main(): startDir is where the go.work
// search begins (os.Getwd() in production, a scratch directory in tests),
// so tests can exercise real `go test -json` subprocesses against a
// throwaway module without an `os.Chdir` that would race other tests.
func run(startDir string, args []string, stdout, stderr io.Writer) error {
	goWork, err := findGoWork(startDir)
	if err != nil {
		return err
	}
	pattern := "./..."
	if len(args) > 0 {
		pattern = args[0]
	}

	variants := []struct {
		name string
		env  envVariant
	}{
		{"scalar", envVariant{simd: false, forceScalar: false}},
		{"simd", envVariant{simd: true, forceScalar: false}},
		{"simd-forced-scalar", envVariant{simd: true, forceScalar: true}},
	}

	suites := make([]suiteResult, len(variants))
	for i, v := range variants {
		fmt.Fprintf(stdout, "running %s suite...\n", v.name)
		suites[i] = runSuite(goWork, pattern, v.env, stderr)
	}

	names := make([]string, len(variants))
	for i, v := range variants {
		names[i] = v.name
	}

	problem := report(stdout, names, suites)
	for i, s := range suites {
		if s.processErr != nil {
			fmt.Fprintf(stderr, "%s suite process error: %v\n", names[i], s.processErr)
			problem = true
		}
		if s.parseIssue != "" {
			fmt.Fprintf(stderr, "%s suite output parse issue: %s\n", names[i], s.parseIssue)
			problem = true
		}
	}

	if problem {
		return fmt.Errorf("differential run found problems (see above)")
	}
	return nil
}

func findGoWork(startDir string) (string, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, "go.work")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.work not found above %s", dir)
		}
		dir = parent
	}
}

type packageResult struct {
	pass   bool
	ran    bool
	output strings.Builder
}

// suiteResult is the full outcome of one `go test -json` run: the
// per-package results actually parsed, plus anything that should make the
// run untrustworthy even if every parsed package happened to pass.
type suiteResult struct {
	packages map[string]*packageResult

	// processErr is set for anything other than "the test binary exited
	// non-zero because tests failed" -- e.g. the go command itself could
	// not be started, or exited via a signal. A plain failing-tests exit
	// is expected and is instead visible through packages[...].pass.
	processErr error

	// parseIssue is set if the JSON stream could not be trusted: a
	// scanner error (including a token too long for the buffer) or a line
	// that looked like it should have been a JSON test event but wasn't.
	parseIssue string
}

type envVariant struct {
	simd        bool
	forceScalar bool
}

func runSuite(goWork, pattern string, variant envVariant, stderr io.Writer) suiteResult {
	cmd := exec.Command("go", "test", "-json", pattern)
	cmd.Dir = filepath.Dir(goWork)
	cmd.Env = goEnv(goWork, variant)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return suiteResult{packages: map[string]*packageResult{}, processErr: err}
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return suiteResult{packages: map[string]*packageResult{}, processErr: err}
	}

	packages, parseIssue := parsePackageResults(stdout)
	waitErr := cmd.Wait()

	result := suiteResult{packages: packages, parseIssue: parseIssue}
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			// Not a plain "tests failed" exit (e.g. the binary could not
			// even run): this is a hard process error, independent of
			// whatever packages did get parsed.
			result.processErr = waitErr
		}
	}
	return result
}

// testEvent covers both shapes go test -json emits: ordinary test/package
// events key their subject as "Package", but build-output/build-fail
// events (emitted when a package pattern doesn't resolve or fails to
// compile) key it as "ImportPath" instead.
type testEvent struct {
	Action     string `json:"Action"`
	Package    string `json:"Package"`
	ImportPath string `json:"ImportPath"`
	Output     string `json:"Output"`
}

func (e testEvent) subject() string {
	if e.Package != "" {
		return e.Package
	}
	return e.ImportPath
}

// parsePackageResults reads a go test -json stream. It returns a non-empty
// parseIssue if the scanner itself failed, or if a non-empty line could
// not be decoded as a test event at all -- go test -json's stdout is JSON
// lines only, so any line that fails to parse means either a scanner
// desync or a go test output format this parser doesn't understand, and
// silently skipping it would let a corrupted or truncated run report as
// clean.
func parsePackageResults(stdout io.Reader) (map[string]*packageResult, string) {
	results := make(map[string]*packageResult)
	var badLines int
	var firstBadLine string

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			continue
		}
		var event testEvent
		if err := json.Unmarshal(line, &event); err != nil {
			badLines++
			if firstBadLine == "" {
				firstBadLine = trimmed
			}
			continue
		}
		subject := event.subject()
		if subject == "" {
			continue
		}
		result := results[subject]
		if result == nil {
			result = &packageResult{}
			results[subject] = result
		}
		if event.Output != "" {
			result.output.WriteString(event.Output)
		}
		switch event.Action {
		case "pass":
			result.ran = true
			result.pass = true
		case "fail", "build-fail":
			result.ran = true
			result.pass = false
		}
	}

	if err := scanner.Err(); err != nil {
		return results, fmt.Sprintf("scanner error: %v", err)
	}
	if badLines > 0 {
		return results, fmt.Sprintf("%d line(s) did not parse as JSON test events (first: %q)", badLines, firstBadLine)
	}
	return results, ""
}

func goEnv(goWork string, variant envVariant) []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env)+2)
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOEXPERIMENT=") || strings.HasPrefix(entry, "GOWORK=") || strings.HasPrefix(entry, "GODEC_FORCE_SCALAR=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, "GOWORK="+goWork)

	// Always start from an explicit, empty GOEXPERIMENT baseline (rather
	// than the calling shell's), so the "scalar" variant is never
	// accidentally built with a SIMD-enabling experiment the caller
	// happened to export.
	experiment := ""
	if variant.simd {
		experiment = "simd"
	}
	filtered = append(filtered, "GOEXPERIMENT="+experiment)

	if variant.forceScalar {
		filtered = append(filtered, "GODEC_FORCE_SCALAR=1")
	}
	return filtered
}

// report prints one row per package covered by any suite, across all
// suites, and returns true if any suite disagrees with the others on a
// package's status (PASS/FAIL/MISSING) -- including the case where every
// suite agrees on FAIL, since a shared failure is still a failure the
// caller must not report as clean.
func report(w io.Writer, names []string, suites []suiteResult) bool {
	packageNames := map[string]struct{}{}
	for _, s := range suites {
		for name := range s.packages {
			packageNames[name] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(packageNames))
	for name := range packageNames {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	problem := false
	totalFail := 0
	mismatched := 0
	for _, pkg := range sorted {
		statuses := make([]string, len(suites))
		for i, s := range suites {
			statuses[i] = status(s.packages[pkg])
		}

		agree := true
		for _, st := range statuses {
			if st != statuses[0] {
				agree = false
			}
			if st == "FAIL" {
				totalFail++
			}
		}

		if agree && statuses[0] == "PASS" {
			continue
		}
		if agree && statuses[0] == "MISSING" {
			// Not covered by any suite (e.g. filtered by build tags in
			// every variant); nothing to compare.
			continue
		}

		problem = true
		if !agree {
			mismatched++
			fmt.Fprintf(w, "[MISMATCH] %s:", pkg)
		} else {
			fmt.Fprintf(w, "[FAIL] %s:", pkg)
		}
		for i, st := range statuses {
			fmt.Fprintf(w, " %s=%s", names[i], st)
		}
		fmt.Fprintln(w)
		for i, s := range suites {
			if r := s.packages[pkg]; r != nil && !r.pass && r.output.Len() > 0 {
				fmt.Fprintf(w, "  %s output: %s\n", names[i], strings.TrimSpace(r.output.String()))
			}
		}
	}

	fmt.Fprintf(w, "\n%d package(s) observed across %d variant(s): %d mismatched, %d fail-somewhere (shared or not)\n",
		len(sorted), len(suites), mismatched, totalFail)
	return problem
}

func status(r *packageResult) string {
	if r == nil || !r.ran {
		return "MISSING"
	}
	if r.pass {
		return "PASS"
	}
	return "FAIL"
}
