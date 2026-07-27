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

func getGodextureDepVersions(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	var versions []string
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
			if len(parts) >= 3 && strings.HasPrefix(parts[1], "github.com/godexture/") {
				versions = append(versions, parts[2])
			}
			continue
		}
		if inRequire && line != "" && !strings.HasPrefix(line, "//") {
			parts := strings.Fields(line)
			if len(parts) >= 2 && strings.HasPrefix(parts[0], "github.com/godexture/") {
				versions = append(versions, parts[1])
			}
		}
	}
	return versions
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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: release <subcommand> [args]")
		fmt.Println("Subcommands:")
		fmt.Println("  commit <patch|minor|major|vX.Y.Z>  Bump versions in go.mod and commit")
		fmt.Println("  push                               Push commits in all submodules")
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
	commonFlags.Parse(os.Args[2:])

	args := commonFlags.Args()

	goWork, err := workspace.ResolveGoWork(goCommand, workPath)
	if err != nil {
		cli.Fatal(err)
	}

	modules, err := workspace.WorkspaceModules(goCommand, goWork)
	if err != nil {
		cli.Fatal(err)
	}
	if len(modules) == 0 {
		cli.Fatalf("no modules found in %s", goWork)
	}

	switch subcommand {
	case "commit":
		if len(args) < 1 {
			cli.Fatalf("commit subcommand requires a version argument (patch, minor, major, vX.Y.Z)")
		}
		arg := args[0]
		
		var targetVersion string
		if arg == "patch" || arg == "minor" || arg == "major" {
			var maxV string
			for _, abs := range modules {
				vers := getGodextureDepVersions(abs)
				for _, v := range vers {
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
			targetVersion = incrementVersion(maxV, arg)
			fmt.Printf("Calculated new version: %s (from max %s)\n", targetVersion, maxV)
		} else if strings.HasPrefix(arg, "v") {
			targetVersion = arg
			fmt.Printf("Using explicit version: %s\n", targetVersion)
		} else {
			cli.Fatalf("invalid version argument: %s", arg)
		}

		for rel, abs := range modules {
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

	case "push":
		for _, abs := range modules {
			fmt.Printf("==> Pushing changes in %s\n", abs)
			cmd := exec.Command("git", "push")
			cmd.Dir = abs
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Printf("Warning: git push failed in %s: %v\n", abs, err)
			}
		}

	case "gh-release":
		if len(args) < 1 {
			cli.Fatalf("gh-release subcommand requires a version argument (vX.Y.Z)")
		}
		targetVersion := args[0]
		if !strings.HasPrefix(targetVersion, "v") {
			cli.Fatalf("invalid version argument: %s", targetVersion)
		}
		for _, abs := range modules {
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
		for rel, abs := range modules {
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
