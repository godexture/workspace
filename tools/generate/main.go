package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/godexture/tools/internal/workspace"
)

func main() {
	log.Printf("starting generate")
	scriptArgs, generateArgs := workspace.SplitArgs(os.Args[1:])

	var workPath string
	var goCommand string
	var test bool

	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)
	flags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	flags.StringVar(&goCommand, "go", "go", "go command to run")
	flags.BoolVar(&test, "test", false, "include test files")
	if err := flags.Parse(scriptArgs); err != nil {
		fatal(err)
	}

	generateArgs = workspace.EnsurePackagePattern(generateArgs, flagNeedsValue)
	passthroughFlags, pkgPattern := workspace.SplitPackagePattern(generateArgs, flagNeedsValue)

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

	tmpDir, err := os.MkdirTemp("", "godexture-gen-*")
	if err != nil {
		fatal(fmt.Errorf("failed to create temp dir: %w", err))
	}
	defer os.RemoveAll(tmpDir)

	var buildWg sync.WaitGroup
	buildErrs := make(chan error, 2)

	buildWg.Add(1)
	go func() {
		defer buildWg.Done()
		log.Printf("building config-generator...")
		buildConfigCmd := exec.Command(goCommand, "build", "-o", filepath.Join(tmpDir, "config-generator.exe"), "github.com/godexture/tools/config-generator")
		buildConfigCmd.Stdout = os.Stdout
		buildConfigCmd.Stderr = os.Stderr
		if err := buildConfigCmd.Run(); err != nil {
			buildErrs <- fmt.Errorf("failed to build config-generator: %w", err)
			return
		}
		log.Printf("built config-generator to %s", tmpDir)
	}()

	buildWg.Add(1)
	go func() {
		defer buildWg.Done()
		log.Printf("pre-warming build cache for table-generator...")
		buildTableCmd := exec.Command(goCommand, "build", "github.com/godexture/tools/table-generator")
		buildTableCmd.Stdout = os.Stdout
		buildTableCmd.Stderr = os.Stderr
		if err := buildTableCmd.Run(); err != nil {
			buildErrs <- fmt.Errorf("failed to build table-generator: %w", err)
			return
		}
		log.Printf("pre-warmed build cache for table-generator")
	}()

	buildWg.Wait()
	close(buildErrs)
	for err := range buildErrs {
		fatal(err)
	}

	originalPath := os.Getenv("PATH")
	newPath := tmpDir + string(os.PathListSeparator) + originalPath
	if err := os.Setenv("PATH", newPath); err != nil {
		fatal(fmt.Errorf("failed to set PATH: %w", err))
	}

	err = runGenerate(goCommand, goWork, passthroughFlags, pkgPattern, test)
	if err != nil {
		os.Exit(1)
	}
	log.Printf("finished generate")
}

func runGenerate(goCommand, goWork string, passthroughFlags, pkgPattern []string, test bool) error {
	listArgs := append([]string{"list", "-json"}, pkgPattern...)
	listCmd := exec.Command(goCommand, listArgs...)
	listCmd.Dir = filepath.Dir(goWork)
	listCmd.Env = append(os.Environ(), "GOWORK="+goWork)

	output, err := listCmd.Output()
	if err != nil {
		return fmt.Errorf("go list failed: %w", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(output)))

	var wg sync.WaitGroup
	errCh := make(chan error, 1000)

	for {
		var pkg struct {
			Dir          string
			GoFiles      []string
			CgoFiles     []string
			TestGoFiles  []string
			XTestGoFiles []string
		}
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		var regularFiles []string
		regularFiles = append(regularFiles, pkg.GoFiles...)
		regularFiles = append(regularFiles, pkg.CgoFiles...)

		if len(regularFiles) > 0 {
			wg.Add(1)
			go func(dir string, files []string) {
				defer wg.Done()
				if err := runGenerateFiles(goCommand, goWork, dir, passthroughFlags, files); err != nil {
					errCh <- err
				}
			}(pkg.Dir, regularFiles)
		}

		if test {
			if len(pkg.TestGoFiles) > 0 {
				wg.Add(1)
				go func(dir string, files []string) {
					defer wg.Done()
					if err := runGenerateFiles(goCommand, goWork, dir, passthroughFlags, files); err != nil {
						errCh <- err
					}
				}(pkg.Dir, pkg.TestGoFiles)
			}

			if len(pkg.XTestGoFiles) > 0 {
				wg.Add(1)
				go func(dir string, files []string) {
					defer wg.Done()
					if err := runGenerateFiles(goCommand, goWork, dir, passthroughFlags, files); err != nil {
						errCh <- err
					}
				}(pkg.Dir, pkg.XTestGoFiles)
			}
		}
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("generate failed with %d errors, first error: %w", len(errs), errs[0])
	}

	return nil
}

func runGenerateFiles(goCommand, goWork, dir string, passthroughFlags, files []string) error {
	args := append([]string{"generate"}, passthroughFlags...)
	args = append(args, files...)

	cmd := exec.Command(goCommand, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK="+goWork)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func flagNeedsValue(flag string) bool {
	name := strings.TrimLeft(strings.SplitN(flag, "=", 2)[0], "-")
	switch name {
	case "run", "tags", "asmflags", "buildmode", "compiler", "gccgoflags", "gcflags", "installsuffix", "ldflags", "mod", "modfile", "overlay", "p", "pkgdir", "toolexec":
		return true
	default:
		return false
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
