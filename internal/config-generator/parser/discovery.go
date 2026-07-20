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
			discoverMetadata(t, allFiles)
			return
		}
	}
	cli.Fatalf("type %s not found in auto-discovery", t.Type)
}

func discoverMetadata(t *types.Target, allFiles []*ast.File) {
	t.FieldChoices = make(map[string][]string)
	for _, field := range t.StructType.Fields.List {
		if len(field.Names) != 1 || !ast.IsExported(field.Names[0].Name) {
			continue
		}
		ident, ok := field.Type.(*ast.Ident)
		if !ok {
			continue
		}
		t.FieldChoices[field.Names[0].Name] = stringConstants(allFiles, t.PackageName, ident.Name)
	}
	for _, file := range allFiles {
		if file.Name.Name != t.PackageName {
			continue
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != "Validate" || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver, ok := function.Recv.List[0].Type.(*ast.Ident)
			if ok && receiver.Name == t.Type {
				t.HasValidate = true
			}
		}
	}
}

func stringConstants(files []*ast.File, packageName, typeName string) []string {
	var values []string
	for _, file := range files {
		if file.Name.Name != packageName {
			continue
		}
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, item := range group.Specs {
				spec, ok := item.(*ast.ValueSpec)
				if !ok || spec.Type == nil || len(spec.Values) != 1 {
					continue
				}
				declaredType, ok := spec.Type.(*ast.Ident)
				literal, literalOK := spec.Values[0].(*ast.BasicLit)
				if !ok || !literalOK || declaredType.Name != typeName || literal.Kind != token.STRING {
					continue
				}
				values = append(values, strings.Trim(literal.Value, "\""))
			}
		}
	}
	return values
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
