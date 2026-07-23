package types

import "go/ast"

type Target struct {
	Type         string
	Source       string
	ResolvedType string
	Default      string
	Preset       string
	ExtraImports map[string]string

	StructType   *ast.StructType
	PackageName  string
	ImportPath   string
	HasValidate  bool
	FieldChoices map[string][]string
	Enums        []EnumInfo
}

type EnumInfo struct {
	TypeName   string
	ConstNames []string
}

type FieldInfo struct {
	Name    string
	TypeStr string
	Targets []*Target
}
