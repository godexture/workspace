//go:build ignore

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type workFile struct {
	Use []struct {
		DiskPath string
	} `json:"Use"`
}

type result struct {
	module   string
	dir      string
	output   string
	duration time.Duration
	err      error
}

func main() {
	scriptArgs, testArgs := splitArgs(os.Args[1:])

	var workPath string
	var goCommand string
	var parallel int
	var showOutput bool

	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)
	flags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	flags.StringVar(&goCommand, "go", "go", "go command to run")
	flags.IntVar(&parallel, "parallel", 0, "maximum number of modules to test at once; 0 means all modules")
	flags.BoolVar(&showOutput, "show-output", false, "print go test output for successful modules too")
	if err := flags.Parse(scriptArgs); err != nil {
		fatal(err)
	}

	testArgs = ensurePackagePattern(testArgs)
	if hasVerboseFlag(testArgs) {
		showOutput = true
	}

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

	if parallel <= 0 || parallel > len(modules) {
		parallel = len(modules)
	}

	fmt.Printf("go.work: %s\n", goWork)
	fmt.Printf("modules: %d, parallel: %d, go test args: %s\n", len(modules), parallel, strings.Join(testArgs, " "))

	results := runTests(goCommand, goWork, modules, testArgs, parallel)

	failed := 0
	for _, res := range results {
		status := "PASS"
		if res.err != nil {
			status = "FAIL"
			failed++
		}
		fmt.Printf("\n[%s] %s (%s)\n", status, res.module, res.duration.Round(time.Millisecond))
		if res.err != nil {
			fmt.Printf("dir: %s\nerror: %v\n", res.dir, res.err)
		}
		if res.output != "" && (showOutput || res.err != nil) {
			fmt.Print(strings.TrimRight(res.output, "\r\n"))
			fmt.Println()
		}
	}

	if failed > 0 {
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

func runTests(goCommand, goWork string, modules map[string]string, testArgs []string, parallel int) []result {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)

	jobs := make(chan string)
	results := make(chan result, len(names))

	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				results <- testModule(goCommand, goWork, name, modules[name], testArgs)
			}
		}()
	}

	for _, name := range names {
		jobs <- name
	}
	close(jobs)
	wg.Wait()
	close(results)

	byModule := make(map[string]result, len(names))
	for res := range results {
		byModule[res.module] = res
	}

	ordered := make([]result, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, byModule[name])
	}
	return ordered
}

func testModule(goCommand, goWork, module, dir string, testArgs []string) result {
	start := time.Now()
	args := append([]string{"test"}, testArgs...)
	cmd := exec.Command(goCommand, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK="+goWork)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	return result{
		module:   module,
		dir:      dir,
		output:   output.String(),
		duration: time.Since(start),
		err:      err,
	}
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

func hasVerboseFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-v" || strings.HasPrefix(arg, "-v=") {
			return true
		}
	}
	return false
}

func ensurePackagePattern(args []string) []string {
	if len(args) == 0 {
		return []string{"./..."}
	}

	argsIndex := len(args)
	for i, arg := range args {
		if arg == "-args" {
			argsIndex = i
			break
		}
	}

	for i := 0; i < argsIndex; i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			return args
		}
		if flagNeedsValue(arg) && !strings.Contains(arg, "=") {
			i++
		}
	}

	withPattern := make([]string, 0, len(args)+1)
	withPattern = append(withPattern, args[:argsIndex]...)
	withPattern = append(withPattern, "./...")
	withPattern = append(withPattern, args[argsIndex:]...)
	return withPattern
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
