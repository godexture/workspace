package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/workspace"
	"github.com/spf13/pflag"
	"golang.org/x/mod/semver"
)

func getGodextureDeps(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	var deps []string
	scanner := bufio.NewScanner(bytes.NewReader(b))
	inRequire := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "require (" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(line, "require ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && strings.HasPrefix(parts[1], "github.com/godexture/") {
				deps = append(deps, parts[1])
			}
			continue
		}
		if inRequire && line != "" && !strings.HasPrefix(line, "//") {
			parts := strings.Fields(line)
			if len(parts) >= 1 && strings.HasPrefix(parts[0], "github.com/godexture/") {
				deps = append(deps, parts[0])
			}
		}
	}
	return deps
}

func resolveTargetVersion(arg string, modules map[string]string) string {
	if strings.HasPrefix(arg, "v") {
		return arg
	}
	var maxV string
	for _, abs := range modules {
		cmd := exec.Command("git", "tag")
		cmd.Dir = abs
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(out))
		for scanner.Scan() {
			v := strings.TrimSpace(scanner.Text())
			if !semver.IsValid(v) {
				continue
			}
			if maxV == "" || semver.Compare(v, maxV) > 0 {
				maxV = v
			}
		}
	}
	if maxV == "" {
		maxV = "v0.0.0"
	}
	return incrementVersion(maxV, arg)
}

func incrementVersion(v, bumpType string) string {
	if !semver.IsValid(v) {
		return v
	}
	canonical := semver.Canonical(v)
	parts := strings.Split(strings.TrimPrefix(canonical, "v"), ".")
	if len(parts) != 3 {
		return v
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	patch, _ := strconv.Atoi(parts[2])

	switch bumpType {
	case "major":
		return fmt.Sprintf("v%d.0.0", major+1)
	case "minor":
		return fmt.Sprintf("v%d.%d.0", major, minor+1)
	case "patch":
		return fmt.Sprintf("v%d.%d.%d", major, minor, patch+1)
	default:
		return v
	}
}

func getGitSubmodules(dir string) (map[string]string, error) {
	cmd := exec.Command("git", "submodule", "status")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	modules := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			path := parts[1]
			absPath := filepath.Join(dir, path)
			modules[path] = absPath
		}
	}
	return modules, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: bulk <subcommand> [args]")
		fmt.Println("Subcommands:")
		fmt.Println("  bump <patch|minor|major|vX.Y.Z>    Bump versions in go.mod and commit")
		fmt.Println("  add [args...]                      Run git add in all submodules with changes")
		fmt.Println("  commit [args...]                   Run git commit in all submodules with changes")
		fmt.Println("  push [args...]                     Run git push in all submodules")
		fmt.Println("  gh-release <vX.Y.Z>                Create GitHub release in all submodules")
		fmt.Println("  sync                               Sync dependencies to @latest")
		os.Exit(1)
	}

	subcommand := os.Args[1]

	var workPath string
	var goCommand string

	commonFlags := pflag.NewFlagSet(subcommand, pflag.ExitOnError)
	commonFlags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	commonFlags.StringVar(&goCommand, "go", "go", "go command to run")
	var passThroughArgs []string
	var args []string
	if subcommand == "commit" || subcommand == "add" || subcommand == "push" {
		// For commands that wrap git, we don't parse commonFlags so we can pass all args.
		passThroughArgs = os.Args[2:]
	} else {
		commonFlags.Parse(os.Args[2:])
		args = commonFlags.Args()
	}

	goWork, err := workspace.ResolveGoWork(goCommand, workPath)
	if err != nil {
		cli.Fatal(err)
	}

	goModules, err := workspace.WorkspaceModules(goCommand, goWork)
	if err != nil {
		cli.Fatal(err)
	}
	if len(goModules) == 0 {
		cli.Fatalf("no modules found in %s", goWork)
	}

	gitSubmodules, err := getGitSubmodules(filepath.Dir(goWork))
	if err != nil {
		cli.Fatal(err)
	}

	switch subcommand {
	case "bump":
		if len(args) < 1 {
			cli.Fatalf("bump subcommand requires a version argument (patch, minor, major, vX.Y.Z)")
		}
		arg := args[0]
		if arg != "patch" && arg != "minor" && arg != "major" && !strings.HasPrefix(arg, "v") {
			cli.Fatalf("invalid version argument: %s", arg)
		}
		targetVersion := resolveTargetVersion(arg, goModules)
		fmt.Printf("Resolved target version: %s\n", targetVersion)

		for rel, abs := range goModules {
			deps := getGodextureDeps(abs)
			if len(deps) == 0 {
				continue
			}
			fmt.Printf("==> Bumping dependencies in %s to %s\n", rel, targetVersion)
			for _, dep := range deps {
				cmd := exec.Command(goCommand, "mod", "edit", "-require="+dep+"@"+targetVersion)
				cmd.Dir = abs
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					cli.Fatalf("go mod edit failed in %s: %v", rel, err)
				}
			}

			fmt.Printf("==> Committing changes in %s\n", abs)
			cmd := exec.Command("git", "add", "go.mod")
			cmd.Dir = abs
			cmd.Run()
			
			cmd = exec.Command("git", "add", "go.sum")
			cmd.Dir = abs
			cmd.Run()

			cmd = exec.Command("git", "commit", "-m", "chore: bump dependencies to "+targetVersion)
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: git commit failed in %s (maybe no changes?): %v\n", abs, err)
			}
		}

	case "commit":
		for _, abs := range gitSubmodules {
			// Check if there are changes
			statusCmd := exec.Command("git", "status", "--porcelain")
			statusCmd.Dir = abs
			out, err := statusCmd.Output()
			if err != nil {
				continue
			}
			if len(bytes.TrimSpace(out)) == 0 {
				continue
			}

			fmt.Printf("==> Committing changes in %s\n", abs)
			gitArgs := append([]string{"commit"}, passThroughArgs...)
			cmd := exec.Command("git", gitArgs...)
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: git commit failed in %s: %v\n", abs, err)
			}
		}

	case "add":
		for _, abs := range gitSubmodules {
			// Check if there are changes
			statusCmd := exec.Command("git", "status", "--porcelain")
			statusCmd.Dir = abs
			out, err := statusCmd.Output()
			if err != nil {
				continue
			}
			if len(bytes.TrimSpace(out)) == 0 {
				continue
			}

			fmt.Printf("==> Adding changes in %s\n", abs)
			gitArgs := append([]string{"add"}, passThroughArgs...)
			cmd := exec.Command("git", gitArgs...)
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: git add failed in %s: %v\n", abs, err)
			}
		}

	case "push":
		for _, abs := range gitSubmodules {
			fmt.Printf("==> Pushing changes in %s\n", abs)
			gitArgs := append([]string{"push"}, passThroughArgs...)
			cmd := exec.Command("git", gitArgs...)
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: git push failed in %s: %v\n", abs, err)
			}
		}

	case "gh-release":
		if len(args) < 1 {
			cli.Fatalf("gh-release subcommand requires a version argument (patch, minor, major, vX.Y.Z)")
		}
		arg := args[0]
		if arg != "patch" && arg != "minor" && arg != "major" && !strings.HasPrefix(arg, "v") {
			cli.Fatalf("invalid version argument: %s", arg)
		}
		targetVersion := resolveTargetVersion(arg, goModules)
		fmt.Printf("Resolved target version for release: %s\n", targetVersion)
		for _, abs := range goModules {
			fmt.Printf("==> Creating GitHub release for %s in %s\n", targetVersion, abs)
			cmd := exec.Command("gh", "release", "create", targetVersion, "--generate-notes")
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: gh release create failed in %s: %v\n", abs, err)
			}
		}

	case "sync":
		for rel, abs := range goModules {
			var toUpdate []string
			if len(args) > 0 {
				toUpdate = args
			} else {
				deps := getGodextureDeps(abs)
				for _, dep := range deps {
					toUpdate = append(toUpdate, dep+"@latest")
				}
			}

			if len(toUpdate) == 0 {
				continue
			}

			fmt.Printf("==> Updating dependencies in %s\n", rel)
			
			getArgs := append([]string{"get"}, toUpdate...)
			cmd := exec.Command(goCommand, getArgs...)
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				cli.Fatalf("go get failed in %s: %v", rel, err)
			}

			cmd = exec.Command(goCommand, "mod", "tidy")
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				cli.Fatalf("go mod tidy failed in %s: %v", rel, err)
			}
		}

	default:
		cli.Fatalf("unknown subcommand: %s", subcommand)
	}

	fmt.Println("All done.")
}
