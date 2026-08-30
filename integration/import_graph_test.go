package integration_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/godexture/godec"

var officialFamilies = []string{
	modulePath + "/plugin/audio",
	modulePath + "/plugin/file",
	modulePath + "/plugin/id3",
	modulePath + "/plugin/mp4",
	modulePath + "/plugin/pcm/adpcm",
	modulePath + "/plugin/pcm/g711",
	modulePath + "/plugin/pcm/linear",
	modulePath + "/plugin/vorbiscomment",
	modulePath + "/plugin/wave",
}

func TestThirdPartyFixtureImportsOnlyPublicFoundation(t *testing.T) {
	directory := filepath.Join(integrationDirectory(t), "acme")
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

func TestFoundationAndOfficialFamiliesKeepImportDirection(t *testing.T) {
	root := filepath.Dir(integrationDirectory(t))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".git" || relative == "integration" || relative == "standard" || relative == "tools" || relative == "cli" || relative == "cmd" || strings.HasPrefix(entry.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		owner := officialOwner(filepath.ToSlash(relative))
		for _, imported := range parsed.Imports {
			dependency, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if dependency == modulePath+"/standard" || hasPathPrefix(dependency, modulePath+"/integration") {
				t.Errorf("%s imports dependency-graph parent %s", filepath.ToSlash(relative), dependency)
				continue
			}
			for _, family := range officialFamilies {
				if hasPathPrefix(dependency, family) && family != owner {
					t.Errorf("%s imports concrete official family %s", filepath.ToSlash(relative), dependency)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func integrationDirectory(t testing.TB) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source location is unavailable")
	}
	return filepath.Dir(current)
}

func officialOwner(relative string) string {
	for _, family := range officialFamilies {
		familyRelative := strings.TrimPrefix(family, modulePath+"/")
		if hasPathPrefix(relative, familyRelative) {
			return family
		}
	}
	return ""
}

func hasPathPrefix(value, prefix string) bool {
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}
