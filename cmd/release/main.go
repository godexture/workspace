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
	var workPath string
	var goCommand string
	var ghRelease bool

	flags := pflag.NewFlagSet(filepath.Base(os.Args[0]), pflag.ExitOnError)
	flags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	flags.StringVar(&goCommand, "go", "go", "go command to run")
	flags.BoolVar(&ghRelease, "gh-release", false, "actually perform github release (commit, push, tag, gh release create)")

	if err := flags.Parse(os.Args[1:]); err != nil {
		cli.Fatal(err)
	}

	args := flags.Args()

	goWork, err := workspace.ResolveGoWork(goCommand, workPath)
	if err != nil {
		cli.Fatal(err)
	}
	workspaceRoot := filepath.Dir(goWork)

	modules, err := workspace.WorkspaceModules(goCommand, goWork)
	if err != nil {
		cli.Fatal(err)
	}
	if len(modules) == 0 {
		cli.Fatalf("no modules found in %s", goWork)
	}

	var targetVersion string
	var isExplicitBump bool

	if len(args) > 0 {
		arg := args[0]
		if arg == "patch" || arg == "minor" || arg == "major" {
			isExplicitBump = true
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
			isExplicitBump = true
			targetVersion = arg
			fmt.Printf("Using explicit version: %s\n", targetVersion)
		}
	}

	if isExplicitBump {
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
		}

		if ghRelease {
			fmt.Println("==> Running git commit and push...")
			cmd := exec.Command("git", "add", ".")
			cmd.Dir = workspaceRoot
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				cli.Fatalf("git add failed: %v", err)
			}

			cmd = exec.Command("git", "commit", "-m", "chore: release "+targetVersion)
			cmd.Dir = workspaceRoot
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				cli.Fatalf("git commit failed: %v", err)
			}

			cmd = exec.Command("git", "push")
			cmd.Dir = workspaceRoot
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				cli.Fatalf("git push failed: %v", err)
			}

			fmt.Println("==> Creating tags and GitHub releases...")
			for rel := range modules {
				tag := targetVersion
				if rel != "." {
					tag = filepath.ToSlash(rel) + "/" + targetVersion
				}
				
				fmt.Printf("==> Creating release for %s\n", tag)
				cmd = exec.Command("gh", "release", "create", tag, "--generate-notes")
				cmd.Dir = workspaceRoot
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					cli.Fatalf("gh release create failed for %s: %v", tag, err)
				}
			}
		}

	} else {
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
	}

	fmt.Println("All done.")
}
