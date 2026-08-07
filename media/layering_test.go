package media_test

import (
	"go/build"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "github.com/godexture/godec"

func TestDataPlaneDependenciesStayClosed(t *testing.T) {
	root := repositoryRoot(t)
	allowed := map[string]struct{}{
		modulePath + "/media/audio":       {},
		modulePath + "/media/buffer":      {},
		modulePath + "/media/key":         {},
		modulePath + "/media/packet":      {},
		modulePath + "/media/side":        {},
		modulePath + "/media/timing":      {},
		modulePath + "/internal/marker":   {},
		modulePath + "/internal/snapshot": {},
	}
	for _, rootPackage := range []string{
		modulePath + "/media/packet",
		modulePath + "/media/audio",
	} {
		visited := make(map[string]struct{})
		visitDataPlanePackage(t, root, rootPackage, allowed, visited)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine layering test location")
	}
	return filepath.Dir(filepath.Dir(file))
}

func visitDataPlanePackage(t *testing.T, root, importPath string, allowed, visited map[string]struct{}) {
	t.Helper()
	if _, seen := visited[importPath]; seen {
		return
	}
	visited[importPath] = struct{}{}
	if _, ok := allowed[importPath]; !ok {
		t.Errorf("data-plane package %s is outside the allowed closure", importPath)
		return
	}
	directory := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(importPath, modulePath+"/")))
	pkg, err := build.Default.ImportDir(directory, 0)
	if err != nil {
		t.Errorf("load %s: %v", importPath, err)
		return
	}
	for _, dependency := range pkg.Imports {
		if strings.HasPrefix(dependency, modulePath+"/") {
			visitDataPlanePackage(t, root, dependency, allowed, visited)
			continue
		}
		if strings.Contains(strings.Split(dependency, "/")[0], ".") {
			t.Errorf("data-plane package %s imports external dependency %s", importPath, dependency)
		}
	}
}
