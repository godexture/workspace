package generator

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/config-generator/types"
)

// collectFields processes targets and returns a map of fields and their order.
func collectFields(targets []*types.Target) (map[string]*types.FieldInfo, []string) {
	fieldsMap := make(map[string]*types.FieldInfo)
	var fieldOrder []string

	for _, t := range targets {
		for _, field := range t.StructType.Fields.List {
			if len(field.Names) != 1 {
				cli.Fatalf("%s has an unnamed or multi-name field", t.Type)
			}
			fieldName := field.Names[0].Name
			if !ast.IsExported(fieldName) {
				continue
			}

			var typeBuf bytes.Buffer
			format.Node(&typeBuf, token.NewFileSet(), field.Type)
			typeStr := typeBuf.String()

			if info, ok := fieldsMap[fieldName]; ok {
				if info.TypeStr != typeStr {
					cli.Fatalf("field %s has mismatched types across targets: %s vs %s", fieldName, info.TypeStr, typeStr)
				}
				info.Targets = append(info.Targets, t)
			} else {
				fieldsMap[fieldName] = &types.FieldInfo{
					Name:    fieldName,
					TypeStr: typeStr,
					Targets: []*types.Target{t},
				}
				fieldOrder = append(fieldOrder, fieldName)
			}
		}
	}

	return fieldsMap, fieldOrder
}
