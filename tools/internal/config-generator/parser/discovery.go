package parser

import (
	"go/ast"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/config-generator/types"
)

// FindTargetInfo searches through parsed AST files to find the struct definition for the target.
func FindTargetInfo(t *types.Target, allFiles []*ast.File, filePaths map[*ast.File]string, outputPackageName string) {
	for _, f := range allFiles {
		if findTargetInFile(t, f, filePaths[f], outputPackageName) {
			autoDetectDefaultAndPreset(t, allFiles, t.PackageName)
			return
		}
	}
	cli.Fatalf("type %s not found in auto-discovery", t.Type)
}

func findTargetInFile(t *types.Target, f *ast.File, filePath, outputPackageName string) bool {
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != t.Type {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue // Might be a type alias in a generated file, keep searching
			}

			t.StructType = structType
			t.Source = filePath
			t.PackageName = f.Name.Name
			t.ImportPath = getImportPath(filepath.Dir(t.Source))

			if t.PackageName != outputPackageName {
				if t.ResolvedType == "" {
					t.ResolvedType = t.PackageName + "." + t.Type
				}
				t.ExtraImports[t.PackageName] = t.ImportPath
			} else {
				if t.ResolvedType == "" {
					t.ResolvedType = t.Type
				}
			}
			return true
		}
	}
	return false
}

func getImportPath(dir string) string {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}", "./"+dir)
	out, err := cmd.Output()
	if err != nil {
		cli.Fatalf("go list failed for %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}
