//go:build ignore

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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

type result struct {
	module    string
	jobModule string
	pkg       string
	testName  string
	dir       string
	output    string
	duration  time.Duration
	err       error
}

type job struct {
	module      string
	moduleDir   string
	pkg         string
	pkgDir      string
	pkgLabel    string
	testName    string
	wholeModule bool
}

type listPackage struct {
	ImportPath   string   `json:"ImportPath"`
	Dir          string   `json:"Dir"`
	TestGoFiles  []string `json:"TestGoFiles"`
	XTestGoFiles []string `json:"XTestGoFiles"`
	Error        *struct {
		Err string `json:"Err"`
	} `json:"Error"`
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

func clearSlots(lines int) {
	if lines > 0 {
		// カーソルを lines 行上に戻す
		fmt.Printf("\033[%dA", lines)
	}
}

func drawSlots(slots []slotState) {
	width := terminalWidth()
	for i, s := range slots {
		// 行をクリアして行頭から描画
		fmt.Print("\033[2K\r")
		prefix := fmt.Sprintf("\033[36m[Worker %d]\033[0m ", i+1)
		prefixWidth := len(fmt.Sprintf("[Worker %d] ", i+1))

		if s.status == "IDLE" {
			fmt.Printf("%s(Idle)\n", prefix)
			continue
		}

		var info string
		if s.status == "RUNNING" {
			m := s.module
			t := s.test
			if t == "" {
				t = "..."
			}
			combined := fmt.Sprintf("%s : %s", m, t)
			combined = truncate(combined, width-prefixWidth-len("RUNNING "))
			info = fmt.Sprintf("RUNNING %s", combined)
		} else {
			m := s.module
			m = truncate(m, width-prefixWidth-len("PASS    "))
			if s.status == "PASS" {
				info = fmt.Sprintf("\033[32mPASS\033[0m    %s", m)
			} else {
				info = fmt.Sprintf("\033[31mFAIL\033[0m    %s", m)
			}
		}
		fmt.Printf("%s%s\n", prefix, info)
	}
}

func drawProgressLogs(entries []progressEntry) {
	width := terminalWidth()
	for _, entry := range entries {
		fmt.Print("\033[2K\r")
		status := entry.status
		color := "\033[33m"
		if status == "PASS" {
			color = "\033[32m"
		} else if status == "REJECT" {
			color = "\033[31m"
		} else if status == "SKIP" {
			color = "\033[90m"
		}
		label := entry.module
		if entry.test != "" {
			label += " :: " + entry.test
		}
		label = truncate(label, width-len("["+status+"] "))
		fmt.Printf("%s[%s]\033[0m %s\n", color, status, label)
	}
}

func terminalWidth() int {
	if columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && columns > 0 {
		return columns
	}
	if runtime.GOOS == "windows" {
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
	return 80
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

func drawUI(entries []progressEntry, slots []slotState) {
	drawProgressLogs(entries)
	drawSlots(slots)
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
	flags.IntVar(&parallel, "parallel", 0, "maximum concurrent jobs (package × top-level Test); 0 means runtime.NumCPU()")
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

	var jobs []job
	if hasTestSelectionFlag(passthroughFlags) {
		fmt.Fprintln(os.Stderr, "-run/-bench/-fuzz/-list が指定されたため per-test 分割を無効化し module 丸ごと実行にフォールバックします")
		jobs = wholeModuleJobs(modules)
	} else {
		jobs = discoverJobs(goCommand, goWork, modules, pkgPattern)
	}
	if len(jobs) == 0 {
		fatal(errors.New("no test jobs found"))
	}

	if parallel <= 0 || parallel > len(jobs) {
		parallel = runtime.NumCPU()
		if parallel > len(jobs) {
			parallel = len(jobs)
		}
	}

	fmt.Printf("go.work: %s\n", goWork)
	fmt.Printf("modules: %d, jobs: %d, parallel: %d, go test args: %s\n", len(modules), len(jobs), parallel, strings.Join(testArgs, " "))

	uiChan := make(chan uiEvent)
	resultsChan := runTests(goCommand, goWork, jobs, passthroughFlags, pkgPattern, parallel, uiChan)
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interruptChan)

	slots := make([]slotState, parallel)
	for i := range slots {
		slots[i].status = "IDLE"
	}
	var progressEntries []progressEntry

	for i := 0; i < parallel+len(progressEntries); i++ {
		fmt.Println()
	}
	clearSlots(parallel + len(progressEntries))
	drawUI(progressEntries, slots)
	drawnLines := parallel + len(progressEntries)

	failed := 0
	completed := 0
	total := len(jobs)
	moduleTotals := make(map[string]int)
	moduleCompleted := make(map[string]int)
	moduleFailed := make(map[string]bool)
	for _, j := range jobs {
		moduleTotals[j.module]++
	}

	for {
		select {
		case <-interruptChan:
			clearSlots(drawnLines)
			drawUI(progressEntries, slots)
			fmt.Println()
			fmt.Print("\033[?25h")
			os.Exit(130)
		case ev, ok := <-uiChan:
			if !ok {
				uiChan = nil
				break
			}
			slots[ev.WorkerID] = slotState{module: ev.Module, test: ev.Test, status: ev.Status}
			clearSlots(drawnLines)
			if ev.TestStatus == "REJECT" {
				progressModule := ev.ProgressModule
				if progressModule == "" {
					progressModule = ev.Module
				}
				progressEntries = updateProgress(progressEntries, progressModule, ev.Test, ev.TestStatus)
			}
			drawUI(progressEntries, slots)
			drawnLines = parallel + len(progressEntries)
		case res, ok := <-resultsChan:
			if !ok {
				resultsChan = nil
				break
			}
			completed++
			status := "\033[32mPASS\033[0m"
			if res.err != nil {
				status = "\033[31mFAIL\033[0m"
				failed++
			}
			moduleCompleted[res.jobModule]++
			if res.err != nil {
				moduleFailed[res.jobModule] = true
			}
			if moduleCompleted[res.jobModule] == moduleTotals[res.jobModule] && !moduleFailed[res.jobModule] {
				progressEntries = updateProgress(progressEntries, res.jobModule, "", "PASS")
			}
			clearSlots(drawnLines)
			needsOutput := res.err != nil || (res.output != "" && showOutput)
			if needsOutput {
				label := res.module
				if res.testName != "" {
					label += " :: " + res.testName
				}
				fmt.Printf("[%d/%d] [%s] %s (%s)\n", completed, total, status, label, res.duration.Round(time.Millisecond))
				if res.err != nil {
					fmt.Printf("dir: %s\nerror: %v\n", res.dir, res.err)
				}
				if res.output != "" {
					fmt.Print(strings.TrimRight(res.output, "\r\n"))
					fmt.Println()
				}
			}
			drawUI(progressEntries, slots)
			drawnLines = parallel + len(progressEntries)
		}
		if uiChan == nil && resultsChan == nil {
			break
		}
	}
	clearSlots(drawnLines)
	drawUI(progressEntries, slots)
	fmt.Println()
	fmt.Print("\033[?25h")

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

func runTests(goCommand, goWork string, jobs []job, passthroughFlags, pkgPattern []string, parallel int, uiChan chan<- uiEvent) <-chan result {
	jobChan := make(chan job, len(jobs))
	for _, j := range jobs {
		jobChan <- j
	}
	close(jobChan)

	results := make(chan result, len(jobs))

	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		workerID := i
		go func() {
			defer wg.Done()
			for j := range jobChan {
				results <- runJob(goCommand, goWork, j, passthroughFlags, pkgPattern, workerID, uiChan)
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
		close(uiChan)
	}()

	return results
}

func runJob(goCommand, goWork string, j job, passthroughFlags, pkgPattern []string, workerID int, uiChan chan<- uiEvent) result {
	start := time.Now()
	args := append([]string{"test", "-json"}, passthroughFlags...)
	if j.testName != "" {
		args = append(args, "-run", "^"+j.testName+"$")
	}
	var packages []string
	if j.wholeModule || j.pkg == "" {
		packages = pkgPattern
	} else {
		packages = []string{j.pkg}
	}
	args = appendPackageArgs(args, packages)
	cmd := exec.Command(goCommand, args...)
	cmd.Dir = j.moduleDir
	cmd.Env = goEnv(goWork)

	var output bytes.Buffer
	cmd.Stderr = &output

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result{module: j.pkgLabel, jobModule: j.module, pkg: j.pkg, testName: j.testName, dir: resultDir(j), duration: time.Since(start), err: err}
	}

	if err := cmd.Start(); err != nil {
		return result{module: j.pkgLabel, jobModule: j.module, pkg: j.pkg, testName: j.testName, dir: resultDir(j), duration: time.Since(start), err: err}
	}

	uiChan <- uiEvent{WorkerID: workerID, Module: j.pkgLabel, Status: "RUNNING"}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		var te testEvent
		if err := json.Unmarshal(line, &te); err == nil {
			if te.Test != "" {
				testStatus := ""
				displayTest := te.Test
				progressModule := j.pkgLabel
				switch te.Action {
				case "run":
					testStatus = "RUN"
				case "pass":
					testStatus = "PASS"
					displayTest = ""
					progressModule = j.module
				case "fail":
					testStatus = "REJECT"
				case "skip":
					testStatus = "SKIP"
					displayTest = ""
					progressModule = j.module
				}
				if testStatus == "RUN" || testStatus == "REJECT" {
					uiChan <- uiEvent{WorkerID: workerID, Module: j.pkgLabel, Test: displayTest, Status: "RUNNING", TestStatus: testStatus, ProgressModule: progressModule}
				}
			}
			if te.Output != "" {
				output.WriteString(te.Output)
			}
		} else {
			output.Write(line)
			output.WriteByte('\n')
		}
	}

	err = cmd.Wait()
	status := "PASS"
	if err != nil {
		status = "FAIL"
	}
	uiChan <- uiEvent{WorkerID: workerID, Module: j.pkgLabel, Status: status}

	return result{
		module:    j.pkgLabel,
		jobModule: j.module,
		pkg:       j.pkg,
		testName:  j.testName,
		dir:       resultDir(j),
		output:    output.String(),
		duration:  time.Since(start),
		err:       err,
	}
}

func resultDir(j job) string {
	if j.pkgDir != "" {
		return j.pkgDir
	}
	return j.moduleDir
}

func goEnv(goWork string) []string { return append(os.Environ(), "GOWORK="+goWork) }

func wholeModuleJobs(modules map[string]string) []job {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	jobs := make([]job, 0, len(names))
	for _, name := range names {
		jobs = append(jobs, job{module: name, moduleDir: modules[name], pkgLabel: name, wholeModule: true})
	}
	return jobs
}

func discoverModulePackages(goCommand, goWork, dir string, pkgPattern []string) ([]listPackage, error) {
	cmd := exec.Command(goCommand, append([]string{"list", "-json"}, pkgPattern...)...)
	cmd.Dir, cmd.Env = dir, goEnv(goWork)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var packages []listPackage
	for {
		var p listPackage
		decodeErr := decoder.Decode(&p)
		if decodeErr == io.EOF {
			break
		}
		if decodeErr != nil {
			if len(packages) == 0 {
				return nil, errOr(decodeErr, err)
			}
			break
		}
		packages = append(packages, p)
	}
	if len(packages) == 0 && err != nil {
		return nil, err
	}
	return packages, nil
}

func errOr(first, second error) error {
	if second != nil {
		return second
	}
	return first
}

func discoverPackageTests(goCommand, goWork, moduleDir, pkgImportPath string) ([]string, error) {
	cmd := exec.Command(goCommand, "test", "-list", "^Test", pkgImportPath)
	cmd.Dir, cmd.Env = moduleDir, goEnv(goWork)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ok ") || strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "? ") {
			continue
		}
		if strings.HasPrefix(line, "Test") {
			names = append(names, line)
		}
	}
	return names, nil
}

func discoverForModule(goCommand, goWork, module, dir string, pkgPattern []string) []job {
	packages, err := discoverModulePackages(goCommand, goWork, dir, pkgPattern)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: test discovery failed for %s: %v; falling back to whole module\n", module, err)
		return []job{{module: module, moduleDir: dir, pkgLabel: module, wholeModule: true}}
	}
	jobs := make([]job, 0)
	for _, p := range packages {
		if len(p.TestGoFiles) == 0 && len(p.XTestGoFiles) == 0 {
			continue
		}
		label := module
		if p.Dir != "" {
			if rel, relErr := filepath.Rel(dir, p.Dir); relErr == nil && rel != "." {
				label = filepath.ToSlash(filepath.Join(module, rel))
			}
		}
		base := job{module: module, moduleDir: dir, pkg: p.ImportPath, pkgDir: p.Dir, pkgLabel: label}
		if p.Error != nil {
			jobs = append(jobs, base)
			continue
		}
		names, testErr := discoverPackageTests(goCommand, goWork, dir, p.ImportPath)
		if testErr != nil || len(names) == 0 {
			jobs = append(jobs, base)
			continue
		}
		for _, name := range names {
			j := base
			j.testName = name
			jobs = append(jobs, j)
		}
	}
	return jobs
}

func discoverJobs(goCommand, goWork string, modules map[string]string, pkgPattern []string) []job {
	type discovered struct {
		module string
		jobs   []job
	}
	jobsChan := make(chan discovered, len(modules))
	moduleChan := make(chan string)
	workers := runtime.NumCPU()
	if workers > len(modules) {
		workers = len(modules)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for module := range moduleChan {
				jobsChan <- discovered{module, discoverForModule(goCommand, goWork, module, modules[module], pkgPattern)}
			}
		}()
	}
	go func() {
		for module := range modules {
			moduleChan <- module
		}
		close(moduleChan)
		wg.Wait()
		close(jobsChan)
	}()
	var discoveredJobs []job
	for d := range jobsChan {
		discoveredJobs = append(discoveredJobs, d.jobs...)
	}
	sort.SliceStable(discoveredJobs, func(i, j int) bool {
		if discoveredJobs[i].module != discoveredJobs[j].module {
			return discoveredJobs[i].module < discoveredJobs[j].module
		}
		if discoveredJobs[i].pkgLabel != discoveredJobs[j].pkgLabel {
			return discoveredJobs[i].pkgLabel < discoveredJobs[j].pkgLabel
		}
		return discoveredJobs[i].testName < discoveredJobs[j].testName
	})
	return discoveredJobs
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

func hasTestSelectionFlag(args []string) bool {
	for i, arg := range args {
		if arg == "-args" {
			break
		}
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		if name == "run" || name == "bench" || name == "fuzz" || name == "list" {
			return true
		}
		if flagNeedsValue(arg) && !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
		}
	}
	return false
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
