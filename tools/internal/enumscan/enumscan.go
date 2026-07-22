// Package enumscan discovers string-backed enum types and their constant
// values from Go source, and loads a package's files for that purpose. It is
// shared by generators that need to read enum definitions (config-generator's
// FieldChoices, enum-generator's Valid) from the same AST so the discovery
// rules never drift between them.
package enumscan

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/godexture/tools/internal/cli"
)

// Enum is a string-backed named type and the literal values declared for it
// via const blocks in the scanned package.
type Enum struct {
	TypeName string
	Values   []string
}

// LoadPackage parses every non-test .go file in dir, plus determines the
// package name from the files that would remain after excludeFile is removed
// (the file a generator is about to (re)write).
func LoadPackage(dir, excludeFile string) (files []*ast.File, filePaths map[*ast.File]string, packageName string, err error) {
	fileSet := token.NewFileSet()

	packages, err := goparser.ParseDir(fileSet, dir, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go") && info.Name() != excludeFile
	}, goparser.ParseComments)
	if err != nil {
		return nil, nil, "", err
	}
	if len(packages) != 1 {
		cli.Fatalf("expected one package in %s, found %d", dir, len(packages))
	}
	for name := range packages {
		packageName = name
	}

	filePaths = make(map[*ast.File]string)
	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := goparser.ParseFile(fileSet, path, nil, goparser.ParseComments)
		if parseErr == nil {
			files = append(files, f)
			filePaths[f] = path
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, "", walkErr
	}
	return files, filePaths, packageName, nil
}

// StringConstants returns the literal string values assigned, in declaration
// order, to package-level consts of the named type typeName within
// packageName.
func StringConstants(files []*ast.File, packageName, typeName string) []string {
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

// DiscoverStringEnums finds every `type X string` declaration in packageName
// that has at least one associated const value, in source order.
func DiscoverStringEnums(files []*ast.File, packageName string) []Enum {
	var enums []Enum
	for _, file := range files {
		if file.Name.Name != packageName {
			continue
		}
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.TYPE {
				continue
			}
			for _, item := range group.Specs {
				spec, ok := item.(*ast.TypeSpec)
				if !ok {
					continue
				}
				ident, ok := spec.Type.(*ast.Ident)
				if !ok || ident.Name != "string" {
					continue
				}
				values := StringConstants(files, packageName, spec.Name.Name)
				if len(values) == 0 {
					continue
				}
				enums = append(enums, Enum{TypeName: spec.Name.Name, Values: values})
			}
		}
	}
	return enums
}
