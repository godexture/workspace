package generator

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/godexture/tools/internal/config-generator/types"
)

// collectImports extracts all the required imports across all targets.
func collectImports(targets []*types.Target) map[string]string {
	usedImports := make(map[string]string)

	for _, t := range targets {
		for name, path := range t.ExtraImports {
			usedImports[name] = path
		}
		// Parse their source files to get imports that might be used by field types
		fileSet := token.NewFileSet()
		f, err := parser.ParseFile(fileSet, t.Source, nil, parser.ParseComments)
		if err == nil {
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, "\"")
				name := filepath.Base(path)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				usedImports[name] = path
			}
		}
	}

	return usedImports
}
