package parser

import (
	"go/ast"
	"go/token"

	"github.com/godexture/godec/tools/internal/config-generator/types"
)

func autoDetectDefaultAndPreset(t *types.Target, allFiles []*ast.File, pkgName string) {
	for _, f := range allFiles {
		if f.Name.Name != pkgName {
			continue
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				checkGenDecl(t, d, pkgName, f.Name.Name)
			case *ast.FuncDecl:
				checkFuncDecl(t, d, pkgName)
			}
		}
	}
}

func checkGenDecl(t *types.Target, d *ast.GenDecl, expectedPkg, actualPkg string) {
	if d.Tok != token.VAR && d.Tok != token.CONST {
		return
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if ok {
			for _, name := range vs.Names {
				if name.Name == "Default"+t.Type && t.Default == "" {
					if expectedPkg == actualPkg {
						t.Default = expectedPkg + "." + name.Name
					} else {
						t.Default = name.Name
					}
				}
			}
		}
	}
}

func checkFuncDecl(t *types.Target, d *ast.FuncDecl, pkgName string) {
	if d.Name.Name == "Default"+t.Type && t.Default == "" {
		t.Default = pkgName + "." + d.Name.Name + "()"
	}
	if d.Name.Name == "GetPreset" && t.Preset == "" {
		if d.Type.Results != nil && len(d.Type.Results.List) > 0 {
			retType := d.Type.Results.List[0].Type
			if ident, ok := retType.(*ast.Ident); ok && ident.Name == t.Type {
				t.Preset = pkgName + "." + d.Name.Name
			}
		}
	}
}
