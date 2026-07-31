package generator

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"reflect"
	"strings"

	"github.com/godexture/godec/tools/internal/config-generator/types"
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
				tag := ""
				if field.Tag != nil {
					tag = " " + field.Tag.Value
				}
				fmt.Fprintf(body, "%s %s%s\n", field.Names[0].Name, value.String(), tag)
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
		fmt.Fprintf(body, "func %s(options ...%s) (%s, error) {\n", constructorName, optName, configName)
		fmt.Fprintf(body, "\tconfig := %s(%s)\n", configName, initExpr)
		fmt.Fprintf(body, "\tfor _, option := range options {\n\t\toption.apply%s(&config)\n\t}\n", configName)
		fmt.Fprintf(body, "\tif err := config.Validate(); err != nil {\n\t\treturn config, err\n\t}\n")
		fmt.Fprintf(body, "\treturn config, nil\n}\n\n")

		fmt.Fprintf(body, "func Must%s(options ...%s) %s {\n", constructorName, optName, configName)
		fmt.Fprintf(body, "\tconfig, err := %s(options...)\n", constructorName)
		fmt.Fprintf(body, "\tif err != nil {\n\t\tpanic(err)\n\t}\n")
		fmt.Fprintf(body, "\treturn config\n}\n\n")

		fmt.Fprintf(body, "func (c %s) ResolveDefault() %s {\n\treturn %s(%s)\n}\n\n", configName, t.ResolvedType, t.ResolvedType, initExpr)
		fmt.Fprintf(body, "func (c %s) Resolve() %s {\n\treturn %s(c)\n}\n\n", configName, t.ResolvedType, t.ResolvedType)
		
		// Always generate Validate() method
		fmt.Fprintf(body, "func (c %s) Validate() error {\n", configName)
		generateValidationBody(body, t)
		if t.HasValidate {
			fmt.Fprintf(body, "\treturn c.Resolve().Validate()\n")
		} else {
			fmt.Fprintf(body, "\treturn nil\n")
		}
		fmt.Fprintf(body, "}\n\n")

		if len(t.FieldChoices) > 0 {
			fmt.Fprintf(body, "func (c %s) FieldChoices(field string) []string {\n\tswitch field {\n", configName)
			for _, field := range t.StructType.Fields.List {
				if len(field.Names) != 1 {
					continue
				}
				values := t.FieldChoices[field.Names[0].Name]
				if len(values) == 0 {
					continue
				}
				fmt.Fprintf(body, "\tcase %q:\n\t\treturn []string{%s}\n", field.Names[0].Name, quoted(values))
			}
			body.WriteString("\tdefault:\n\t\treturn nil\n\t}\n}\n\n")
		}
	}

	generateEnums(body, targets, packageName)
}

func generateValidationBody(body *bytes.Buffer, t *types.Target) {
	for _, field := range t.StructType.Fields.List {
		if len(field.Names) != 1 || !ast.IsExported(field.Names[0].Name) {
			continue
		}
		fieldName := field.Names[0].Name
		
		if field.Tag == nil {
			continue
		}
		structTag := fieldTag(field)

		checkTag, checkOk := structTag.Lookup("check")
		dependsOnTag, dependsOk := structTag.Lookup("depends-on")
		
		if !checkOk && !dependsOk {
			continue
		}
		
		if dependsOk {
			parts := strings.SplitN(dependsOnTag, "=", 2)
			if len(parts) == 2 {
				depField := parts[0]
				depValues := strings.Split(parts[1], ",")
				
				depGoField := findGoFieldNameByTagName(t.StructType, depField)
				if depGoField == "" {
					depGoField = strings.ToUpper(depField[:1]) + depField[1:]
				}
				fmt.Fprintf(body, "\tif ")
				for i, val := range depValues {
					if i > 0 {
						fmt.Fprintf(body, " || ")
					}
					fmt.Fprintf(body, "string(c.%s) == %q", depGoField, val)
				}
				fmt.Fprintf(body, " {\n")
			}
		}
		
		indent := "\t"
		if dependsOk {
			indent = "\t\t"
		}
		
		if checkOk {
			checks := strings.Split(checkTag, ",")
			for _, check := range checks {
				switch check {
				case "finite":
					fmt.Fprintf(body, "%sif math.IsNaN(float64(c.%s)) || math.IsInf(float64(c.%s), 0) {\n", indent, fieldName, fieldName)
					fmt.Fprintf(body, "%s\treturn fmt.Errorf(\"%%s must be finite\", %q)\n", indent, getCliFieldName(field, fieldName))
					fmt.Fprintf(body, "%s}\n", indent)
				case "positive":
					fmt.Fprintf(body, "%sif !(c.%s > 0) {\n", indent, fieldName)
					fmt.Fprintf(body, "%s\treturn fmt.Errorf(\"%%s must be positive\", %q)\n", indent, getCliFieldName(field, fieldName))
					fmt.Fprintf(body, "%s}\n", indent)
				case "nonnegative":
					fmt.Fprintf(body, "%sif !(c.%s >= 0) {\n", indent, fieldName)
					fmt.Fprintf(body, "%s\treturn fmt.Errorf(\"%%s must be non-negative\", %q)\n", indent, getCliFieldName(field, fieldName))
					fmt.Fprintf(body, "%s}\n", indent)
				}
			}
		}
		
		if dependsOk {
			fmt.Fprintf(body, "\t}\n")
		}
	}
}

func fieldTag(field *ast.Field) reflect.StructTag {
	if field.Tag == nil {
		return ""
	}
	return reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
}

func findGoFieldNameByTagName(structType *ast.StructType, tagName string) string {
	for _, field := range structType.Fields.List {
		if len(field.Names) == 1 {
			if nameTag, ok := fieldTag(field).Lookup("name"); ok && nameTag == tagName {
				return field.Names[0].Name
			}
		}
	}
	return ""
}

func getCliFieldName(field *ast.Field, fallback string) string {
	if nameTag, ok := fieldTag(field).Lookup("name"); ok {
		return nameTag
	}
	return fallback
}

func generateEnums(body *bytes.Buffer, targets []*types.Target, packageName string) {
	seenEnums := make(map[string]bool)
	for _, t := range targets {
		if t.PackageName == packageName {
			continue
		}
		for _, enum := range t.Enums {
			if seenEnums[enum.TypeName] {
				continue
			}
			seenEnums[enum.TypeName] = true
			fmt.Fprintf(body, "type %s = %s.%s\n\n", enum.TypeName, t.PackageName, enum.TypeName)
			if len(enum.ConstNames) > 0 {
				fmt.Fprintf(body, "const (\n")
				for _, name := range enum.ConstNames {
					fmt.Fprintf(body, "\t%s = %s.%s\n", name, t.PackageName, name)
				}
				fmt.Fprintf(body, ")\n\n")
			}
		}
	}
}

func quoted(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = fmt.Sprintf("%q", value)
	}
	return strings.Join(quoted, ", ")
}
