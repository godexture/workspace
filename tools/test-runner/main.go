package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godexture/tools/internal/workspace"
)

type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

func main() {
	scriptArgs, testArgs := workspace.SplitArgs(os.Args[1:])

	var workPath string
	var goCommand string
	var parallel int
	var simd bool

	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)
	flags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	flags.StringVar(&goCommand, "go", "go", "go command to run")
	flags.IntVar(&parallel, "parallel", 0, "maximum concurrent tests in each test binary; 0 uses Go's default")
	flags.BoolVar(&simd, "simd", false, "enable GOEXPERIMENT=simd for the test build")
	if err := flags.Parse(scriptArgs); err != nil {
		fatal(err)
	}

	testArgs = workspace.EnsurePackagePattern(testArgs, flagNeedsValue)
	passthroughFlags, pkgPattern := workspace.SplitPackagePattern(testArgs, flagNeedsValue)

	goWork, err := workspace.ResolveGoWork(goCommand, workPath)
	if err != nil {
		fatal(err)
	}

	modules, err := workspace.WorkspaceModules(goCommand, goWork)
	if err != nil {
		fatal(err)
	}
	if len(modules) == 0 {
		fatal(fmt.Errorf("no modules found in %s", goWork))
	}
	pkgPattern = workspace.WorkspacePackagePatterns(modules, pkgPattern)

	err = runTests(goCommand, goWork, passthroughFlags, pkgPattern, parallel, simd)
	if err != nil {
		os.Exit(1)
	}
}

func runTests(goCommand, goWork string, passthroughFlags, pkgPattern []string, parallel int, simd bool) error {
	args := append([]string{"test", "-json"}, passthroughFlags...)
	if parallel > 0 && !hasFlag(args, "parallel") {
		args = append(args, "-parallel", strconv.Itoa(parallel))
	}
	args = workspace.AppendPackageArgs(args, pkgPattern)

	cmd := exec.Command(goCommand, args...)
	cmd.Dir = filepath.Dir(goWork)
	cmd.Env = goEnv(goWork, simd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	packageOutput := make(map[string]string)
	testOutput := make(map[string]map[string]string)
	packageHasFailedTests := make(map[string]bool)

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event testEvent
		if json.Unmarshal(line, &event) != nil {
			fmt.Println(string(line))
			continue
		}
		if event.Package == "" {
			continue
		}

		if event.Output != "" {
			packageOutput[event.Package] += event.Output
			if event.Test != "" {
				if testOutput[event.Package] == nil {
					testOutput[event.Package] = make(map[string]string)
				}
				testOutput[event.Package][event.Test] += event.Output
			}
		}

		switch event.Action {
		case "fail":
			if event.Test != "" {
				packageHasFailedTests[event.Package] = true
				fmt.Printf("[FAIL] %s: %s (%.4fs)\n", event.Package, event.Test, event.Elapsed)
				if out := testOutput[event.Package][event.Test]; out != "" {
					fmt.Println(strings.TrimRight(out, "\r\n"))
					fmt.Println()
				}
			} else {
				if !packageHasFailedTests[event.Package] {
					fmt.Printf("[FAIL] %s (%.4fs)\n", event.Package, event.Elapsed)
					if out := packageOutput[event.Package]; out != "" {
						fmt.Println(strings.TrimRight(out, "\r\n"))
						fmt.Println()
					}
				}
			}
		case "pass":
			if event.Test == "" {
				fmt.Printf("[PASS] %s (%.4fs)\n", event.Package, event.Elapsed)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scanner error: %v\n", err)
	}

	return cmd.Wait()
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-") == name {
			return true
		}
	}
	return false
}

func goEnv(goWork string, simd bool) []string {
	env := append(os.Environ(), "GOWORK="+goWork)
	if !simd {
		return env
	}
	return setEnv(env, "GOEXPERIMENT", enableExperiment(os.Getenv("GOEXPERIMENT"), "simd"))
}

func enableExperiment(value, experiment string) string {
	experiments := strings.Split(value, ",")
	enabled := make([]string, 0, len(experiments)+1)
	for _, current := range experiments {
		current = strings.TrimSpace(current)
		if current == "" || current == experiment || current == "no"+experiment {
			continue
		}
		enabled = append(enabled, current)
	}
	return strings.Join(append(enabled, experiment), ",")
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func flagNeedsValue(flag string) bool {
	name := strings.TrimLeft(strings.SplitN(flag, "=", 2)[0], "-")
	switch name {
	case "benchtime", "blockprofile", "blockprofilerate", "covermode", "coverpkg",
		"count", "cpu", "cpuprofile", "exec", "fuzz", "fuzztime", "list",
		"memprofile", "memprofilerate", "mutexprofile", "mutexprofilefraction",
		"o", "outputdir", "parallel", "run", "shuffle", "skip", "timeout",
		"trace", "vet":
		return true
	default:
		return false
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
