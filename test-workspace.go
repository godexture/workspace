//go:build ignore

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type workFile struct {
	Use []struct {
		DiskPath string
	} `json:"Use"`
}

type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

func main() {
	scriptArgs, testArgs := splitArgs(os.Args[1:])

	var workPath string
	var goCommand string
	var parallel int

	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)
	flags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	flags.StringVar(&goCommand, "go", "go", "go command to run")
	flags.IntVar(&parallel, "parallel", 0, "maximum concurrent tests in each test binary; 0 uses Go's default")
	if err := flags.Parse(scriptArgs); err != nil {
		fatal(err)
	}

	testArgs = ensurePackagePattern(testArgs)
	passthroughFlags, pkgPattern := splitPackagePattern(testArgs)

	goWork, err := resolveGoWork(goCommand, workPath)
	if err != nil {
		fatal(err)
	}

	modules, err := workspaceModules(goCommand, goWork)
	if err != nil {
		fatal(err)
	}
	if len(modules) == 0 {
		fatal(fmt.Errorf("no modules found in %s", goWork))
	}
	pkgPattern = workspacePackagePatterns(modules, pkgPattern)

	err = runTests(goCommand, goWork, passthroughFlags, pkgPattern, parallel)
	if err != nil {
		os.Exit(1)
	}
}

func splitArgs(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func resolveGoWork(goCommand, explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}

	if output, err := exec.Command(goCommand, "env", "GOWORK").Output(); err == nil {
		path := strings.TrimSpace(string(output))
		if path != "" && path != "off" {
			if abs, err := filepath.Abs(path); err == nil {
				return abs, nil
			}
			return path, nil
		}
	}

	if path, err := findUpward("go.work", mustGetwd()); err == nil {
		return path, nil
	}

	if _, source, _, ok := runtime.Caller(0); ok {
		if path, err := findUpward("go.work", filepath.Dir(source)); err == nil {
			return path, nil
		}
	}

	return "", errors.New("go.work not found; run from the workspace or pass -work")
}

func workspaceModules(goCommand, goWork string) (map[string]string, error) {
	cmd := exec.Command(goCommand, "work", "edit", "-json", goWork)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("go work edit failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}

	var parsed workFile
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, err
	}

	baseDir := filepath.Dir(goWork)
	modules := make(map[string]string, len(parsed.Use))
	for _, use := range parsed.Use {
		if use.DiskPath == "" {
			continue
		}
		path := filepath.FromSlash(use.DiskPath)
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(baseDir, abs)
		if err != nil {
			rel = abs
		}
		modules[filepath.ToSlash(rel)] = abs
	}
	return modules, nil
}

func runTests(goCommand, goWork string, passthroughFlags, pkgPattern []string, parallel int) error {
	args := append([]string{"test", "-json"}, passthroughFlags...)
	if parallel > 0 && !hasFlag(args, "parallel") {
		args = append(args, "-parallel", strconv.Itoa(parallel))
	}
	args = appendPackageArgs(args, pkgPattern)

	cmd := exec.Command(goCommand, args...)
	cmd.Dir = filepath.Dir(goWork)
	cmd.Env = goEnv(goWork)

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

func goEnv(goWork string) []string { return append(os.Environ(), "GOWORK="+goWork) }

func workspacePackagePatterns(modules map[string]string, patterns []string) []string {
	if len(patterns) != 1 || patterns[0] != "./..." {
		return patterns
	}
	modulesByPath := make([]string, 0, len(modules))
	for module := range modules {
		modulesByPath = append(modulesByPath, module)
	}
	sort.Strings(modulesByPath)
	patterns = make([]string, 0, len(modulesByPath))
	for _, module := range modulesByPath {
		patterns = append(patterns, "./"+module+"/...")
	}
	return patterns
}

func findUpward(name, start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	return wd
}

func ensurePackagePattern(args []string) []string {
	if len(args) == 0 {
		return []string{"./..."}
	}

	start, argsIndex := findPatternBounds(args)
	if start < argsIndex {
		return args
	}

	withPattern := make([]string, 0, len(args)+1)
	withPattern = append(withPattern, args[:argsIndex]...)
	withPattern = append(withPattern, "./...")
	withPattern = append(withPattern, args[argsIndex:]...)
	return withPattern
}

func argsSeparatorIndex(args []string) int {
	for i, arg := range args {
		if arg == "-args" {
			return i
		}
	}
	return len(args)
}

func findPatternBounds(args []string) (start, argsIndex int) {
	argsIndex = argsSeparatorIndex(args)
	i := 0
	for i < argsIndex {
		arg := args[i]
		if arg == "" {
			i++
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			break
		}
		if flagNeedsValue(arg) && !strings.Contains(arg, "=") {
			i++
		}
		i++
	}
	return i, argsIndex
}

func splitPackagePattern(args []string) (flags, pattern []string) {
	start, argsIndex := findPatternBounds(args)
	flags = append(flags, args[:start]...)
	pattern = append(pattern, args[start:argsIndex]...)
	flags = append(flags, args[argsIndex:]...)
	return flags, pattern
}

func appendPackageArgs(args, packages []string) []string {
	separator := argsSeparatorIndex(args)
	if separator == len(args) {
		return append(args, packages...)
	}
	result := make([]string, 0, len(args)+len(packages))
	result = append(result, args[:separator]...)
	result = append(result, packages...)
	result = append(result, args[separator:]...)
	return result
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
