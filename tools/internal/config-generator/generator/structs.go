package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"strings"

	"github.com/godexture/tools/internal/config-generator/types"
)

// generateTargetStructs generates the core structs, option interfaces, and constructor functions.
func generateTargetStructs(body *bytes.Buffer, targets []*types.Target, packageName string) {
	for _, t := range targets {
		configName := t.Type
		if t.ResolvedType != "" && configName != t.ResolvedType && !strings.Contains(t.ResolvedType, ".") {
			// It's in the same package but an alias
			fmt.Fprintf(body, "type %s %s\n\n", configName, t.ResolvedType)
		} else if t.PackageName == packageName && t.ResolvedType == t.Type {
			// Struct definition in same package
			fmt.Fprintf(body, "type %s struct {\n", configName)
			for _, field := range t.StructType.Fields.List {
				if len(field.Names) != 1 || !ast.IsExported(field.Names[0].Name) {
					continue
				}
				var value bytes.Buffer
				format.Node(&value, token.NewFileSet(), field.Type)
				fmt.Fprintf(body, "%s %s\n", field.Names[0].Name, value.String())
			}
			body.WriteString("}\n\n")
		} else if t.ResolvedType != "" && strings.Contains(t.ResolvedType, ".") {
			fmt.Fprintf(body, "type %s %s\n\n", configName, t.ResolvedType)
		}

		optName := configName + "Option"
		fmt.Fprintf(body, "type %s interface {\n\tapply%s(*%s)\n}\n\n", optName, configName, configName)

		fmt.Fprintf(body, "type %sFunc func(*%s)\n", strings.ToLower(optName[:1])+optName[1:], configName)
		fmt.Fprintf(body, "func (f %sFunc) apply%s(c *%s) {\n\tf(c)\n}\n\n", strings.ToLower(optName[:1])+optName[1:], configName, configName)

		initExpr := configName + "{}"
		if t.Default != "" {
			initExpr = t.Default
		}

		constructorName := "New" + configName
		fmt.Fprintf(body, "func %s(options ...%s) %s {\n", constructorName, optName, configName)
		fmt.Fprintf(body, "\tconfig := %s(%s)\n", configName, initExpr)
		fmt.Fprintf(body, "\tfor _, option := range options {\n\t\toption.apply%s(&config)\n\t}\n", configName)
		fmt.Fprintf(body, "\treturn config\n}\n\n")

		fmt.Fprintf(body, "func (c %s) ResolveDefault() %s {\n\treturn %s(%s)\n}\n\n", configName, t.ResolvedType, t.ResolvedType, initExpr)
		fmt.Fprintf(body, "func (c %s) Resolve() %s {\n\treturn %s(c)\n}\n\n", configName, t.ResolvedType, t.ResolvedType)
	}
}
