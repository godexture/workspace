//go:build ignore

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type workFile struct {
	Use []struct {
		DiskPath string
	} `json:"Use"`
}

type testEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

type uiEvent struct {
	WorkerID       int
	Module         string
	Test           string
	Status         string
	TestStatus     string
	ProgressModule string
	PackageDone    bool
	Output         string
}

type result struct {
	output   string
	duration time.Duration
	err      error
}

type slotState struct {
	module string
	test   string
	status string
}

type progressEntry struct {
	module string
	test   string
	status string
}

type termSize struct {
	mu     sync.Mutex
	width  int
	height int
}

func (size *termSize) get() (int, int) {
	size.mu.Lock()
	defer size.mu.Unlock()
	return size.width, size.height
}

func (size *termSize) set(width, height int) {
	size.mu.Lock()
	defer size.mu.Unlock()
	size.width = width
	size.height = height
}

func runningTests(slots []slotState) []slotState {
	running := make([]slotState, 0, len(slots))
	for _, slot := range slots {
		if slot.status == "RUNNING" {
			running = append(running, slot)
		}
	}
	return running
}

func drawRunningTests(tests []slotState, limit, width int) {
	if limit <= 0 {
		return
	}
	if limit > len(tests) {
		limit = len(tests)
	}
	for i, s := range tests[:limit] {
		last := i == limit-1
		fmt.Print("\033[2K\r")
		m := s.module
		t := s.test
		if t == "" {
			t = "..."
		}
		combined := truncate(fmt.Sprintf("%s : %s", m, t), width-len("[RUNNING] "))
		fmt.Printf("\033[36m[RUNNING]\033[0m %s", combined)
		if !last {
			fmt.Print("\n")
		}
	}
}

func dashboardWorkerCount(total, height int) int {
	available := height - 3
	if available < 0 {
		available = 0
	}
	if available > total {
		return total
	}
	return available
}

func drawProgressLogsLimit(entries []progressEntry, limit, width int) {
	start := 0
	if limit < len(entries) {
		start = len(entries) - limit
	}
	for _, entry := range entries[start:] {
		fmt.Print("\033[2K\r")
		status := entry.status
		color := "\033[90m"
		if status == "PASS" {
			color = "\033[32m"
		} else if status == "REJECT" {
			color = "\033[31m"
		}
		label := entry.module
		if entry.test != "" {
			label += " :: " + entry.test
		}
		label = truncate(label, width-len("["+status+"] "))
		fmt.Printf("%s[%s]\033[0m %s\n", color, status, label)
	}
}

func updateProgress(entries []progressEntry, module, test, status string) []progressEntry {
	const maxEntries = 12
	for i := range entries {
		if entries[i].module == module && entries[i].test == test {
			entries[i].status = status
			return entries
		}
	}
	entries = append(entries, progressEntry{module: module, test: test, status: status})
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
	return entries
}

func terminalWidth() int {
	if runtime.GOOS == "windows" {
		if output, err := exec.Command("powershell", "-NoProfile", "-Command", "$Host.UI.RawUI.WindowSize.Width").Output(); err == nil {
			if columns, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil && columns > 0 {
				return columns
			}
		}
		if output, err := exec.Command("cmd", "/c", "mode", "con").Output(); err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && strings.EqualFold(strings.TrimSuffix(fields[0], ":"), "Columns") {
					if columns, err := strconv.Atoi(fields[1]); err == nil && columns > 0 {
						return columns
					}
				}
			}
		}
	}
	if columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && columns > 0 {
		return columns
	}
	return 80
}

func terminalHeight() int {
	if lines, err := strconv.Atoi(strings.TrimSpace(os.Getenv("LINES"))); err == nil && lines > 0 {
		return lines
	}
	if runtime.GOOS == "windows" {
		if output, err := exec.Command("powershell", "-NoProfile", "-Command", "$Host.UI.RawUI.WindowSize.Height").Output(); err == nil {
			if lines, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil && lines > 0 {
				return lines
			}
		}
		if output, err := exec.Command("cmd", "/c", "mode", "con").Output(); err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && strings.EqualFold(strings.TrimSuffix(fields[0], ":"), "Lines") {
					if lines, err := strconv.Atoi(fields[1]); err == nil && lines > 0 {
						return lines
					}
				}
			}
		}
	}
	return 24
}

func truncate(value string, width int) string {
	if width < 4 {
		return value[:0]
	}
	if len(value) <= width {
		return value
	}
	return value[:width-3] + "..."
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
	flags.IntVar(&parallel, "parallel", 0, "maximum concurrent tests in each test binary; 0 uses Go's default")
	flags.BoolVar(&showOutput, "show-output", false, "print go test output for successful modules too")
	if err := flags.Parse(scriptArgs); err != nil {
		fatal(err)
	}

	testArgs = ensurePackagePattern(testArgs)
	passthroughFlags, pkgPattern := splitPackagePattern(testArgs)
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
	pkgPattern = workspacePackagePatterns(modules, pkgPattern)

	size := termSize{width: terminalWidth(), height: terminalHeight()}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			size.set(terminalWidth(), terminalHeight())
		}
	}()

	uiChan := make(chan uiEvent, 1024)
	resultsChan := runTests(goCommand, goWork, passthroughFlags, pkgPattern, parallel, runtime.NumCPU(), uiChan)
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interruptChan)

	slotCount := runtime.NumCPU()
	if slotCount < 1 {
		slotCount = 1
	}
	slots := make([]slotState, slotCount)
	for i := range slots {
		slots[i].status = "IDLE"
	}
	failed := 0
	completed := 0
	total := 0
	var progressEntries []progressEntry
	var resultHistory []string
	failureOutputShown := false
	dirty := false
	renderTicker := time.NewTicker(100 * time.Millisecond)
	defer renderTicker.Stop()
	fmt.Print("\033[?1049h\033[?25l")
	leaveDashboard := func() {
		fmt.Print("\033[?25h\033[?1049l")
	}
	render := func() {
		width, height := size.get()
		fmt.Print("\033[H")
		fmt.Print("\033[2K\r")
		fmt.Printf("go.work: %s\n", goWork)
		fmt.Print("\033[2K\r")
		running := runningTests(slots)
		fmt.Printf("modules: %d, packages: %d/%d, running: %d, go test args: %s\n", len(modules), completed, total, len(running), strings.Join(testArgs, " "))
		runningLimit := dashboardWorkerCount(len(running), height)
		availableLogs := height - 2 - runningLimit
		if availableLogs < 1 && len(progressEntries) > 0 {
			availableLogs = 1
			runningLimit = height - 3
			if runningLimit < 0 {
				runningLimit = 0
			}
		}
		drawProgressLogsLimit(progressEntries, availableLogs, width)
		drawRunningTests(running, runningLimit, width)
		fmt.Print("\033[J")
	}
	render()

	for {
		select {
		case <-interruptChan:
			render()
			leaveDashboard()
			os.Exit(130)
		case ev, ok := <-uiChan:
			if !ok {
				uiChan = nil
				break
			}
			if ev.WorkerID >= 0 {
				for len(slots) <= ev.WorkerID {
					slots = append(slots, slotState{status: "IDLE"})
				}
				slots[ev.WorkerID] = slotState{module: ev.Module, test: ev.Test, status: ev.Status}
			}
			if ev.Status == "RUNNING" && ev.Test == "" && !ev.PackageDone {
				total++
			}
			if ev.PackageDone {
				completed++
				status := "PASS"
				if ev.Status == "FAIL" {
					status = "REJECT"
					failed++
					resultHistory = append(resultHistory, fmt.Sprintf("[%s] %s", status, ev.Module))
					if ev.Output != "" {
						failureOutputShown = true
						resultHistory = append(resultHistory, strings.TrimRight(ev.Output, "\r\n"))
					}
				}
				progressEntries = updateProgress(progressEntries, ev.Module, "", status)
			}
			if ev.TestStatus == "REJECT" {
				progressModule := ev.ProgressModule
				if progressModule == "" {
					progressModule = ev.Module
				}
				progressEntries = updateProgress(progressEntries, progressModule, ev.Test, "REJECT")
			}
			dirty = true
		case res, ok := <-resultsChan:
			if !ok {
				resultsChan = nil
				break
			}
			if res.err != nil && total == 0 {
				failed++
			}
			status := "PASS"
			if res.err != nil {
				status = "FAIL"
			}
			needsOutput := res.err != nil || (res.output != "" && showOutput)
			if needsOutput {
				resultHistory = append(resultHistory, fmt.Sprintf("[go test] [%s] (%s)", status, res.duration.Round(time.Millisecond)))
				if res.err != nil {
					resultHistory = append(resultHistory, fmt.Sprintf("error: %v", res.err))
				}
				if res.output != "" && (res.err == nil || !failureOutputShown) {
					resultHistory = append(resultHistory, strings.TrimRight(res.output, "\r\n"))
				}
			}
			if len(resultHistory) > 100 {
				resultHistory = resultHistory[len(resultHistory)-100:]
			}
			dirty = true
		case <-renderTicker.C:
			if dirty {
				render()
				dirty = false
			}
		}
		if uiChan == nil && resultsChan == nil {
			break
		}
	}
	render()
	leaveDashboard()
	for _, line := range resultHistory {
		fmt.Println(line)
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

func runTests(goCommand, goWork string, passthroughFlags, pkgPattern []string, parallel, slotCount int, uiChan chan<- uiEvent) <-chan result {
	results := make(chan result, 1)
	go func() {
		defer close(results)
		defer close(uiChan)
		start := time.Now()
		args := append([]string{"test", "-json"}, passthroughFlags...)
		if parallel > 0 && !hasFlag(args, "parallel") {
			args = append(args, "-parallel", strconv.Itoa(parallel))
		}
		if !hasFlag(args, "p") {
			args = append(args, "-p", strconv.Itoa(slotCount))
		}
		args = appendPackageArgs(args, pkgPattern)

		cmd := exec.Command(goCommand, args...)
		cmd.Dir = filepath.Dir(goWork)
		cmd.Env = goEnv(goWork)
		var output bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			results <- result{output: output.String(), duration: time.Since(start), err: err}
			return
		}
		if err := cmd.Start(); err != nil {
			results <- result{output: output.String(), duration: time.Since(start), err: err}
			return
		}

		active := make(map[string]int)
		packageOutput := make(map[string]string)
		free := make([]int, slotCount)
		for i := range free {
			free[i] = i
		}
		nextWorkerID := slotCount
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var event testEvent
			if json.Unmarshal(scanner.Bytes(), &event) != nil {
				output.Write(scanner.Bytes())
				output.WriteByte('\n')
				continue
			}
			if event.Package == "" {
				continue
			}
			workerID, ok := active[event.Package]
			switch event.Action {
			case "start":
				if !ok {
					if len(free) > 0 {
						workerID = free[0]
						free = free[1:]
					} else {
						workerID = nextWorkerID
						nextWorkerID++
					}
					active[event.Package] = workerID
				}
				uiChan <- uiEvent{WorkerID: workerID, Module: event.Package, Status: "RUNNING"}
			case "run":
				if ok {
					uiChan <- uiEvent{WorkerID: workerID, Module: event.Package, Test: event.Test, Status: "RUNNING", TestStatus: "RUN"}
				}
			case "fail":
				if event.Test != "" && ok {
					uiChan <- uiEvent{WorkerID: workerID, Module: event.Package, Test: event.Test, Status: "RUNNING", TestStatus: "REJECT"}
				}
				if event.Test == "" && ok {
					uiChan <- uiEvent{WorkerID: workerID, Module: event.Package, Status: "FAIL", PackageDone: true, Output: packageOutput[event.Package]}
					delete(active, event.Package)
					free = append(free, workerID)
				}
			case "pass", "skip":
				if event.Test == "" && ok {
					uiChan <- uiEvent{WorkerID: workerID, Module: event.Package, Status: "PASS", PackageDone: true}
					delete(active, event.Package)
					free = append(free, workerID)
				} else if event.Test != "" && ok && event.Action == "skip" {
					uiChan <- uiEvent{WorkerID: workerID, Module: event.Package, Test: event.Test, Status: "RUNNING", TestStatus: "SKIP"}
				}
			}
			if event.Output != "" {
				output.WriteString(event.Output)
				packageOutput[event.Package] += event.Output
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			output.WriteString(scanErr.Error())
		}
		err = cmd.Wait()
		output.WriteString(stderr.String())
		results <- result{output: output.String(), duration: time.Since(start), err: err}
	}()
	return results
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

/*
	The package and test discovery helpers were intentionally removed. The go
	command now owns package expansion, build ordering, and concurrency.
*/

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
