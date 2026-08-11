package integration_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestThirdPartyFixtureImportsOnlyPublicFoundation(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source location is unavailable")
	}
	directory := filepath.Join(filepath.Dir(current), "acme")
	packages, err := parser.ParseDir(token.NewFileSet(), directory, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, parsed := range packages {
		for filename, file := range parsed.Files {
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(path, "/internal/") || path == "github.com/godexture/godec/standard" || path == "github.com/godexture/godec/testkit" || strings.HasPrefix(path, "github.com/godexture/godec/plugin/") {
					t.Errorf("%s directly imports disallowed package %s", filepath.Base(filename), path)
				}
			}
		}
	}
}
