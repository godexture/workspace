package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	typeName := flag.String("type", "", "struct type to generate options for")
	sourcePath := flag.String("source", "", "source file containing the struct type")
	extraImport := flag.String("import", "", "additional import in name=path form")
	configName := flag.String("config-name", "", "generated config type name")
	optionName := flag.String("option-name", "", "generated option type name")
	resolvedType := flag.String("resolved-type", "", "resolved config type")
	defaultExpr := flag.String("default", "", "resolved config default expression")
	presetFunc := flag.String("preset", "", "preset factory function")
	presetNormalizer := flag.String("preset-normalizer", "", "function to normalize the preset level")
	output := flag.String("output", "config_options.go", "generated file name")

	flag.Parse()

	if *typeName == "" {
		fatal("-type is required")
	}

	if *optionName == "" {
		*optionName = fmt.Sprintf("%sOption", *typeName)
	}
	if *resolvedType != "" && *defaultExpr == "" {
		*defaultExpr = fmt.Sprintf("%s{}", *resolvedType)
	}

	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go") && info.Name() != *output
	}, parser.ParseComments)
	if err != nil {
		fatal("parse package: %v", err)
	}
	if len(packages) != 1 {
		fatal("expected one package, found %d", len(packages))
	}
	if *sourcePath != "" {
		source, err := parser.ParseFile(fileSet, *sourcePath, nil, parser.ParseComments)
		if err != nil {
			fatal("parse source: %v", err)
		}
		for _, declaration := range source.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != *typeName {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					fatal("%s is not a struct", *typeName)
				}
				for _, pkg := range packages {
					generate(*output, pkg.Name, *typeName, *optionName, *configName, *resolvedType, *defaultExpr, *presetFunc, *presetNormalizer, *extraImport, true, structType, source)
					return
				}
			}
		}
		fatal("type %s not found in %s", *typeName, *sourcePath)
	}

	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, declaration := range file.Decls {
				gen, ok := declaration.(*ast.GenDecl)
				if !ok || gen.Tok != token.TYPE {
					continue
				}
				for _, spec := range gen.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok || typeSpec.Name.Name != *typeName {
						continue
					}
					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						fatal("%s is not a struct", *typeName)
					}
					generate(*output, pkg.Name, *typeName, *optionName, *configName, *resolvedType, *defaultExpr, *presetFunc, *presetNormalizer, *extraImport, false, structType, file)
					return
				}
			}
		}
	}
	fatal("type %s not found", *typeName)
}

func generate(output, packageName, typeName, optionName, configName, resolvedType, defaultExpr, presetFunc, presetNormalizer, extraImport string, emitConfig bool, structType *ast.StructType, source *ast.File) {
	if configName == "" {
		configName = typeName
	}
	imports := importNames(source)
	if extraImport != "" {
		name, path, ok := strings.Cut(extraImport, "=")
		if !ok || name == "" || path == "" {
			fatal("-import must be name=path")
		}
		imports[name] = path
	}
	usedImports := map[string]string{}
	var body bytes.Buffer

	isAlias := resolvedType != "" && configName != resolvedType

	if isAlias {
		fmt.Fprintf(&body, "type %s %s\n\n", configName, resolvedType)
	} else if resolvedType == "" && (emitConfig || configName != typeName) {
		fmt.Fprintf(&body, "type %s struct {\n", configName)
		for _, field := range structType.Fields.List {
			if len(field.Names) != 1 {
				fatal("%s has an unnamed or multi-name field", typeName)
			}

			fieldName := field.Names[0].Name
			if !ast.IsExported(fieldName) {
				continue
			}

			collectImports(field.Type, imports, usedImports)
			var value bytes.Buffer
			if err := format.Node(&value, token.NewFileSet(), field.Type); err != nil {
				fatal("format %s: %v", fieldName, err)
			}
			fmt.Fprintf(&body, "%s %s\n", fieldName, value.String())
		}
		body.WriteString("}\n\n")
	}

	initExpr := configName + "{}"
	if defaultExpr != "" {
		initExpr = defaultExpr
	}

	constructorName := "New" + configName

	fmt.Fprintf(&body, "type %s func(*%s)\n\n", optionName, configName)
	fmt.Fprintf(&body, "func %s(options ...%s) %s {", constructorName, optionName, configName)
	fmt.Fprintf(&body, "config := %s(%s)\n", configName, initExpr)
	fmt.Fprintf(&body, "for _, option := range options { option(&config) }\n")
	body.WriteString("return config\n")
	fmt.Fprintf(&body, "}\n")

	for _, field := range structType.Fields.List {
		if len(field.Names) != 1 {
			fatal("%s has an unnamed or multi-name field", typeName)
		}

		fieldName := field.Names[0].Name
		if !ast.IsExported(fieldName) {
			continue
		}

		collectImports(field.Type, imports, usedImports)
		var value bytes.Buffer
		if err := format.Node(&value, token.NewFileSet(), field.Type); err != nil {
			fatal("format %s: %v", fieldName, err)
		}
		fmt.Fprintf(&body, "\nfunc With%s(v %s) %s {\n", fieldName, value.String(), optionName)
		fmt.Fprintf(&body, "return func(c *%s) {\n", configName)
		fmt.Fprintf(&body, "c.%s = v\n", fieldName)
		body.WriteString("}\n}\n")
	}
	if (resolvedType == "") && (defaultExpr != "") {
		fatal("-resolved-type must be specified when using -default")
	}
	collectTextImports(resolvedType+" "+defaultExpr+" "+presetFunc+" "+presetNormalizer, imports, usedImports)

	if presetFunc != "" {
		level := "level"
		if presetNormalizer != "" {
			level = presetNormalizer + "(level)"
		}

		fmt.Fprintf(&body, "\nfunc WithPreset(level int) %s {\n", optionName)
		fmt.Fprintf(&body, "return func(c *%s) {\n", configName)
		fmt.Fprintf(&body, "*c = %s(%s(%s))\n", configName, presetFunc, level)
		fmt.Fprintf(&body, "}\n")
		body.WriteString("}\n")
	}

	fmt.Fprintf(&body, "\n func (c %s) ResolveDefault() %s {\nreturn %s\n}\n", configName, resolvedType, initExpr)
	fmt.Fprintf(&body, "\nfunc (c %s) Resolve() %s {\nreturn %s(c)\n}\n", configName, resolvedType, resolvedType)

	var sourceCode bytes.Buffer
	sourceCode.WriteString("// Code generated by config-generator. DO NOT EDIT.\n\n")
	fmt.Fprintf(&sourceCode, "package %s\n", packageName)
	if len(usedImports) > 0 {
		names := make([]string, 0, len(usedImports))
		for name := range usedImports {
			names = append(names, name)
		}
		sort.Strings(names)
		sourceCode.WriteString("\nimport (\n")
		for _, name := range names {
			fmt.Fprintf(&sourceCode, "%s %s\n", name, strconv.Quote(usedImports[name]))
		}
		sourceCode.WriteString(")\n")
	}
	sourceCode.WriteString("\n")
	sourceCode.WriteString(body.String())

	formatted, err := format.Source(sourceCode.Bytes())
	if err != nil {
		fatal("format generated source: %v", err)
	}
	if err := os.WriteFile(output, formatted, 0o644); err != nil {
		fatal("write %s: %v", output, err)
	}
}

func collectTextImports(text string, imports map[string]string, used map[string]string) {
	for name, path := range imports {
		if strings.Contains(text, name+".") {
			used[name] = path
		}
	}
}

func importNames(file *ast.File) map[string]string {
	imports := map[string]string{}
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		name := filepath.Base(path)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = path
	}
	return imports
}

func collectImports(expr ast.Expr, imports map[string]string, used map[string]string) {
	ast.Inspect(expr, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := imports[identifier.Name]; ok {
			used[identifier.Name] = path
		}
		return true
	})
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "config-generator: "+format+"\n", args...)
	os.Exit(1)
}
