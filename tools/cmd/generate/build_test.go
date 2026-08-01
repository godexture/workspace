package main

import (
	"runtime"
	"testing"
)

// TestExeNameMatchesPlatformExpectation pins docs/refactor/checkpoint.md
// M1-R1: PATH lookup for a bare //go:generate command name (config-generator,
// enum-generator) requires the .exe suffix on Windows but requires its
// absence on Unix -- a hardcoded ".exe" in buildTools broke generate on
// Unix even though the binary itself built and ran fine when invoked
// directly by path.
func TestExeNameMatchesPlatformExpectation(t *testing.T) {
	got := exeName("config-generator")
	want := "config-generator"
	if runtime.GOOS == "windows" {
		want = "config-generator.exe"
	}
	if got != want {
		t.Fatalf("exeName(%q) = %q, want %q on GOOS=%s", "config-generator", got, want, runtime.GOOS)
	}
}
