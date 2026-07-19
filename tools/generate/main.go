package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/godexture/tools/internal/workspace"
)

func main() {
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

	err = runGenerate(goCommand, goWork, passthroughFlags, pkgPattern, test)
	if err != nil {
		os.Exit(1)
	}
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
			if err := runGenerateFiles(goCommand, goWork, pkg.Dir, passthroughFlags, regularFiles); err != nil {
				return err
			}
		}

		if test {
			if len(pkg.TestGoFiles) > 0 {
				if err := runGenerateFiles(goCommand, goWork, pkg.Dir, passthroughFlags, pkg.TestGoFiles); err != nil {
					return err
				}
			}

			if len(pkg.XTestGoFiles) > 0 {
				if err := runGenerateFiles(goCommand, goWork, pkg.Dir, passthroughFlags, pkg.XTestGoFiles); err != nil {
					return err
				}
			}
		}
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
