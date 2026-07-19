package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type workFile struct {
	Use []struct {
		DiskPath string
	} `json:"Use"`
}

func ResolveGoWork(goCommand, explicit string) (string, error) {
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

	if wd, err := os.Getwd(); err == nil {
		if path, err := findUpward("go.work", wd); err == nil {
			return path, nil
		}
	}

	if _, source, _, ok := runtime.Caller(0); ok {
		if path, err := findUpward("go.work", filepath.Dir(source)); err == nil {
			return path, nil
		}
	}

	return "", errors.New("go.work not found; run from the workspace or pass -work")
}

func WorkspaceModules(goCommand, goWork string) (map[string]string, error) {
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

func WorkspacePackagePatterns(modules map[string]string, patterns []string) []string {
	if len(patterns) != 1 || patterns[0] != "./..." {
		return patterns
	}
	modulesByPath := make([]string, 0, len(modules))
	for module := range modules {
		modulesByPath = append(modulesByPath, module)
	}
	sort.Strings(modulesByPath)
	result := make([]string, 0, len(modulesByPath))
	for _, module := range modulesByPath {
		result = append(result, "./"+module+"/...")
	}
	return result
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
