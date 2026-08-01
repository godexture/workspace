package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoEnvScalarStripsInheritedSIMDExperiment(t *testing.T) {
	t.Setenv("GOEXPERIMENT", "simd,rangefunc")
	env := goEnv("/repo/go.work", envVariant{simd: false})
	got := lookupEnv(t, env, "GOEXPERIMENT")
	if got != "" {
		t.Fatalf("scalar GOEXPERIMENT = %q, want empty (must not inherit simd from the caller)", got)
	}
}

func TestGoEnvSIMDSetsExperimentRegardlessOfInherited(t *testing.T) {
	t.Setenv("GOEXPERIMENT", "")
	env := goEnv("/repo/go.work", envVariant{simd: true})
	if got := lookupEnv(t, env, "GOEXPERIMENT"); got != "simd" {
		t.Fatalf("SIMD GOEXPERIMENT = %q, want %q", got, "simd")
	}
}

func TestGoEnvForceScalarSetsMarkerVariable(t *testing.T) {
	env := goEnv("/repo/go.work", envVariant{simd: true, forceScalar: true})
	if got := lookupEnv(t, env, "GODEC_FORCE_SCALAR"); got != "1" {
		t.Fatalf("GODEC_FORCE_SCALAR = %q, want %q", got, "1")
	}
	env = goEnv("/repo/go.work", envVariant{simd: true, forceScalar: false})
	if _, ok := lookupEnvOK(env, "GODEC_FORCE_SCALAR"); ok {
		t.Fatal("GODEC_FORCE_SCALAR should be unset when forceScalar is false")
	}
}

func TestGoEnvDropsInheritedForceScalarWhenNotRequested(t *testing.T) {
	t.Setenv("GODEC_FORCE_SCALAR", "1")
	env := goEnv("/repo/go.work", envVariant{simd: true, forceScalar: false})
	if _, ok := lookupEnvOK(env, "GODEC_FORCE_SCALAR"); ok {
		t.Fatal("inherited GODEC_FORCE_SCALAR must not leak into a variant that didn't request it")
	}
}

func lookupEnv(t *testing.T, env []string, key string) string {
	t.Helper()
	v, _ := lookupEnvOK(env, key)
	return v
}

func lookupEnvOK(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix), true
		}
	}
	return "", false
}

func TestParsePackageResultsHandlesImportPathAndPackageKeys(t *testing.T) {
	stream := strings.Join([]string{
		`{"ImportPath":"./missing","Action":"build-output","Output":"boom\n"}`,
		`{"ImportPath":"./missing","Action":"build-fail"}`,
		`{"Package":"./missing","Action":"start"}`,
		`{"Package":"./missing","Action":"fail"}`,
		`{"Package":"./ok","Action":"pass"}`,
		"",
	}, "\n")

	results, parseIssue := parsePackageResults(strings.NewReader(stream))
	if parseIssue != "" {
		t.Fatalf("parseIssue = %q, want empty", parseIssue)
	}
	if status(results["./missing"]) != "FAIL" {
		t.Fatalf("./missing status = %s, want FAIL", status(results["./missing"]))
	}
	if status(results["./ok"]) != "PASS" {
		t.Fatalf("./ok status = %s, want PASS", status(results["./ok"]))
	}
}

func TestParsePackageResultsFlagsUnparseableLines(t *testing.T) {
	stream := `{"Package":"./ok","Action":"pass"}` + "\n" + "not json at all\n"
	_, parseIssue := parsePackageResults(strings.NewReader(stream))
	if parseIssue == "" {
		t.Fatal("parseIssue should be non-empty when a line fails to parse as a test event")
	}
}

func TestReportFlagsSharedFailureAsAProblem(t *testing.T) {
	suites := []suiteResult{
		{packages: map[string]*packageResult{"./pkg": {ran: true, pass: false}}},
		{packages: map[string]*packageResult{"./pkg": {ran: true, pass: false}}},
	}
	var buf bytes.Buffer
	if problem := report(&buf, []string{"a", "b"}, suites); !problem {
		t.Fatal("report() = false, want true: both suites failing the same package identically must still be a problem")
	}
}

func TestReportCleanWhenAllSuitesPass(t *testing.T) {
	suites := []suiteResult{
		{packages: map[string]*packageResult{"./pkg": {ran: true, pass: true}}},
		{packages: map[string]*packageResult{"./pkg": {ran: true, pass: true}}},
	}
	var buf bytes.Buffer
	if problem := report(&buf, []string{"a", "b"}, suites); problem {
		t.Fatalf("report() = true, want false when every suite agrees PASS; output:\n%s", buf.String())
	}
}

func TestReportFlagsMismatchBetweenSuites(t *testing.T) {
	suites := []suiteResult{
		{packages: map[string]*packageResult{"./pkg": {ran: true, pass: true}}},
		{packages: map[string]*packageResult{"./pkg": {ran: true, pass: false}}},
	}
	var buf bytes.Buffer
	if problem := report(&buf, []string{"a", "b"}, suites); !problem {
		t.Fatal("report() = false, want true when suites disagree on a package's status")
	}
	if !strings.Contains(buf.String(), "[MISMATCH]") {
		t.Fatalf("report output missing [MISMATCH] marker:\n%s", buf.String())
	}
}

// --- integration tests: real `go test -json` subprocesses against a
// throwaway scratch module, covering docs/refactor/checkpoint.md M0-R1's
// named negative case.

func TestRunReportsNonZeroOnSharedFailure(t *testing.T) {
	dir := scratchModule(t, `package pkg

import "testing"

func TestAlwaysFails(t *testing.T) {
	t.Fatal("boom")
}
`)
	var stdout, stderr bytes.Buffer
	err := run(dir, []string{"./..."}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run() succeeded, want an error: a package failing identically under every build variant must not report success\nstdout:\n%s", stdout.String())
	}
}

func TestRunCleanOnAllPass(t *testing.T) {
	dir := scratchModule(t, `package pkg

import "testing"

func TestAlwaysPasses(t *testing.T) {}
`)
	var stdout, stderr bytes.Buffer
	if err := run(dir, []string{"./..."}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v, want nil\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}

func TestRunReportsNonZeroOnNonexistentPackage(t *testing.T) {
	dir := scratchModule(t, `package pkg
`)
	var stdout, stderr bytes.Buffer
	err := run(dir, []string{"./does-not-exist"}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run() succeeded for a package pattern matching nothing, want an error\nstdout:\n%s", stdout.String())
	}
}

func TestAnyPackageFailedRequiresAFailedPackage(t *testing.T) {
	if anyPackageFailed(map[string]*packageResult{}) {
		t.Fatal("anyPackageFailed() = true for an empty map, want false")
	}
	if anyPackageFailed(map[string]*packageResult{"./pkg": {ran: true, pass: true}}) {
		t.Fatal("anyPackageFailed() = true when every parsed package passed, want false")
	}
	if !anyPackageFailed(map[string]*packageResult{"./pkg": {ran: true, pass: false}}) {
		t.Fatal("anyPackageFailed() = false when a parsed package failed, want true")
	}
}

// TestRunReportsNonZeroWhenGoTestExitsBeforeAnyTestEvent reproduces
// docs/refactor/checkpoint.md's named repro for the optional tool's false
// positive: GOFLAGS=-definitely-invalid makes every `go test` invocation
// exit non-zero before printing a single JSON test event (go rejects the
// flag before it even resolves packages), so the old runSuite logic --
// treating any *exec.ExitError as "tests failed, and that's reflected in
// packages" -- couldn't distinguish this from "0 packages, nothing wrong"
// and run() reported success despite go test never having run anything.
func TestRunReportsNonZeroWhenGoTestExitsBeforeAnyTestEvent(t *testing.T) {
	dir := scratchModule(t, `package pkg

import "testing"

func TestAlwaysPasses(t *testing.T) {}
`)
	t.Setenv("GOFLAGS", "-definitely-invalid")
	var stdout, stderr bytes.Buffer
	err := run(dir, []string{"./..."}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run() succeeded despite go test exiting before any test event, want an error\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

// scratchModule writes a minimal module + workspace containing pkgSource
// as pkg/pkg.go (or pkg_test.go if it declares tests) under t.TempDir()
// and returns the module root.
func scratchModule(t *testing.T, pkgSource string) string {
	t.Helper()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module scratch\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26\n\nuse .\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(root, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "pkg.go"
	if strings.Contains(pkgSource, "testing.T") {
		name = "pkg_test.go"
	}
	if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(pkgSource), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
