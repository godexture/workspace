// Command differential runs the workspace test suite twice -- once with
// the default (scalar) toolchain and once with GOEXPERIMENT=simd -- and
// prints a single joined report comparing the two, instead of two
// independent pass/fail runs a reader has to compare by hand.
//
// docs/refactor/checkpoint.md M0#4: "同一inputに対するcross-build result
// が一つのreportに出る" -- see docs/refactor/performance.md's note that
// `./test-runner.exe --simd` alone "SIMD build を一度走らせるだけで、
// scalar/SIMD artifact の横断比較を保証しない".
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

type packageResult struct {
	pass   bool
	ran    bool
	output strings.Builder
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "differential:", err)
		os.Exit(1)
	}
}

func run() error {
	goWork, err := findGoWork()
	if err != nil {
		return err
	}

	fmt.Println("running scalar suite...")
	scalar, scalarErr := runSuite(goWork, false)
	fmt.Println("running SIMD suite...")
	simd, simdErr := runSuite(goWork, true)

	mismatch := report(scalar, simd)

	if scalarErr != nil {
		fmt.Fprintln(os.Stderr, "scalar suite process error:", scalarErr)
	}
	if simdErr != nil {
		fmt.Fprintln(os.Stderr, "SIMD suite process error:", simdErr)
	}
	if mismatch || scalarErr != nil || simdErr != nil {
		os.Exit(1)
	}
	return nil
}

func findGoWork() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
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

func runSuite(goWork string, simd bool) (map[string]*packageResult, error) {
	pattern := "./..."
	if len(os.Args) > 1 {
		pattern = os.Args[1]
	}
	cmd := exec.Command("go", "test", "-json", pattern)
	cmd.Dir = filepath.Dir(goWork)
	cmd.Env = goEnv(goWork, simd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	results := parsePackageResults(stdout)
	waitErr := cmd.Wait()
	if waitErr != nil {
		if _, ok := waitErr.(*exec.ExitError); !ok {
			return results, waitErr
		}
	}
	return results, nil
}

type testEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Output  string `json:"Output"`
}

func parsePackageResults(stdout io.Reader) map[string]*packageResult {
	results := make(map[string]*packageResult)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event testEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Package == "" {
			continue
		}
		result := results[event.Package]
		if result == nil {
			result = &packageResult{}
			results[event.Package] = result
		}
		if event.Output != "" {
			result.output.WriteString(event.Output)
		}
		switch event.Action {
		case "pass":
			result.ran = true
			result.pass = true
		case "fail":
			result.ran = true
			result.pass = false
		}
	}
	return results
}

func goEnv(goWork string, simd bool) []string {
	env := append(os.Environ(), "GOWORK="+goWork)
	if !simd {
		return env
	}
	experiments := strings.Split(os.Getenv("GOEXPERIMENT"), ",")
	kept := make([]string, 0, len(experiments)+1)
	for _, e := range experiments {
		e = strings.TrimSpace(e)
		if e == "" || e == "simd" || e == "nosimd" {
			continue
		}
		kept = append(kept, e)
	}
	kept = append(kept, "simd")
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GOEXPERIMENT=") {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "GOEXPERIMENT="+strings.Join(kept, ","))
}

// report prints one row per package covered by either run and returns true
// if any package's scalar and SIMD results disagree.
func report(scalar, simd map[string]*packageResult) bool {
	names := make(map[string]struct{}, len(scalar)+len(simd))
	for name := range scalar {
		names[name] = struct{}{}
	}
	for name := range simd {
		names[name] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	mismatch := false
	scalarFail, simdFail, both := 0, 0, 0
	for _, name := range sorted {
		s, sOK := scalar[name]
		x, xOK := simd[name]
		sStatus := status(s, sOK)
		xStatus := status(x, xOK)
		if sStatus == "FAIL" {
			scalarFail++
		}
		if xStatus == "FAIL" {
			simdFail++
		}
		if sStatus == xStatus {
			both++
			continue
		}
		mismatch = true
		fmt.Printf("[MISMATCH] %s: scalar=%s simd=%s\n", name, sStatus, xStatus)
		if sOK && s.output.Len() > 0 && sStatus != "PASS" {
			fmt.Println("  scalar output:", strings.TrimSpace(s.output.String()))
		}
		if xOK && x.output.Len() > 0 && xStatus != "PASS" {
			fmt.Println("  simd output:  ", strings.TrimSpace(x.output.String()))
		}
	}

	fmt.Printf("\n%d packages: %d agree, %d mismatched (scalar failures: %d, SIMD failures: %d)\n",
		len(sorted), both, len(sorted)-both, scalarFail, simdFail)
	return mismatch
}

func status(r *packageResult, ok bool) string {
	if !ok || !r.ran {
		return "MISSING"
	}
	if r.pass {
		return "PASS"
	}
	return "FAIL"
}
