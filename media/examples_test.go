package media_test

import (
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var foundationExampleDirectories = []string{
	"access",
	"config",
	"diagnostic",
	"endpoint",
	"flow",
	"host",
	"job",
	"plugin",
	"resource",
	"media/audio",
	"media/buffer",
	"media/carrier",
	"media/codec",
	"media/format",
	"media/key",
	"media/metadata",
	"media/packet",
	"media/property",
	"media/schema",
	"media/side",
	"media/stream",
	"media/tag",
	"media/timing",
}

func TestFoundationPackagesHaveExamples(t *testing.T) {
	root := repositoryRoot(t)
	for _, relative := range foundationExampleDirectories {
		relative := relative
		t.Run(filepath.ToSlash(relative), func(t *testing.T) {
			directory := filepath.Join(root, filepath.FromSlash(relative))
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			var files []*ast.File
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
					continue
				}
				file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, entry.Name()), nil, parser.ParseComments)
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, file)
			}
			found := false
			for _, example := range doc.Examples(files...) {
				if example.Output != "" || example.EmptyOutput {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("foundation package %s has no executable Example", relative)
			}
		})
	}
}
