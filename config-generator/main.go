package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type stringSlice []string

func (i *stringSlice) String() string { return strings.Join(*i, ",") }
func (i *stringSlice) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type Target struct {
	Type         string
	Source       string
	ResolvedType string
	Default      string
	Preset       string
	ExtraImports map[string]string

	StructType  *ast.StructType
	PackageName string
	ImportPath  string
}

func main() {
	var targetsFlag stringSlice
	flag.Var(&targetsFlag, "target", "target configurations (e.g., EncoderConfig,default=...)")
	outputFlag := flag.String("output", "", "generated file name (default: {GOFILE}_options.go)")

	flag.Parse()

	if len(targetsFlag) == 0 {
		fatal("at least one -target is required")
	}

	output := *outputFlag
	if output == "" {
		gofile := os.Getenv("GOFILE")
		if gofile != "" {
			output = strings.TrimSuffix(gofile, ".go") + "_options.go"
		} else {
			output = "config_options.go"
		}
	}

	targets := make([]*Target, 0, len(targetsFlag))
	for _, s := range targetsFlag {
		targets = append(targets, parseTarget(s))
	}

	fileSet := token.NewFileSet()

	// Parse current package to get the output package name
	packages, err := parser.ParseDir(fileSet, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go") && info.Name() != output
	}, parser.ParseComments)
	if err != nil {
		fatal("parse package: %v", err)
	}
	if len(packages) != 1 {
		fatal("expected one package in current directory, found %d", len(packages))
	}
	var outputPackageName string
	for name := range packages {
		outputPackageName = name
	}

	// Walk all go files
	var allFiles []*ast.File
	filePaths := make(map[*ast.File]string)
	filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err == nil {
			allFiles = append(allFiles, f)
			filePaths[f] = path
		}
		return nil
	})

	for _, t := range targets {
		findTargetInfo(t, allFiles, filePaths, outputPackageName)
	}

	generate(output, outputPackageName, targets)
}

func parseTarget(s string) *Target {
	parts := strings.Split(s, ",")
	t := &Target{Type: parts[0], ExtraImports: make(map[string]string)}
	for _, part := range parts[1:] {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			switch kv[0] {
			case "default":
				t.Default = kv[1]
			case "preset":
				t.Preset = kv[1]
			case "source":
				t.Source = kv[1]
			case "resolved-type":
				t.ResolvedType = kv[1]
			case "import":
				ikv := strings.SplitN(kv[1], "=", 2)
				if len(ikv) == 2 {
					t.ExtraImports[ikv[0]] = ikv[1]
				}
			}
		}
	}
	return t
}

func findTargetInfo(t *Target, allFiles []*ast.File, filePaths map[*ast.File]string, outputPackageName string) {
	for _, f := range allFiles {
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
				t.Source = filePaths[f]
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

				autoDetectDefaultAndPreset(t, allFiles, t.PackageName)
				return
			}
		}
	}
	fatal("type %s not found in auto-discovery", t.Type)
}

func getImportPath(dir string) string {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}", "./"+dir)
	out, err := cmd.Output()
	if err != nil {
		fatal("go list failed for %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func autoDetectDefaultAndPreset(t *Target, allFiles []*ast.File, pkgName string) {
	for _, f := range allFiles {
		if f.Name.Name != pkgName {
			continue
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok == token.VAR || d.Tok == token.CONST {
					for _, spec := range d.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if ok {
							for _, name := range vs.Names {
								if name.Name == "Default"+t.Type && t.Default == "" {
									if pkgName == f.Name.Name {
										t.Default = pkgName + "." + name.Name
									} else {
										t.Default = name.Name
									}
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
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
		}
	}
}

type FieldInfo struct {
	Name    string
	TypeStr string
	Targets []*Target
}

func generate(output, packageName string, targets []*Target) {
	absOutput, _ := filepath.Abs(output)
	if absOutput == "" {
		absOutput = output
	}
	log.Printf("generating %s...", absOutput)
	defer log.Printf("generated %s", absOutput)
	var body bytes.Buffer
	usedImports := make(map[string]string)

	// Collect imports from all targets
	for _, t := range targets {
		for name, path := range t.ExtraImports {
			usedImports[name] = path
		}
		// Also parse their source files to get imports that might be used by field types
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

	fieldsMap := make(map[string]*FieldInfo)
	var fieldOrder []string

	for _, t := range targets {
		for _, field := range t.StructType.Fields.List {
			if len(field.Names) != 1 {
				fatal("%s has an unnamed or multi-name field", t.Type)
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
					fatal("field %s has mismatched types across targets: %s vs %s", fieldName, info.TypeStr, typeStr)
				}
				info.Targets = append(info.Targets, t)
			} else {
				fieldsMap[fieldName] = &FieldInfo{
					Name:    fieldName,
					TypeStr: typeStr,
					Targets: []*Target{t},
				}
				fieldOrder = append(fieldOrder, fieldName)
			}
		}
	}

	// Generate target boilerplate
	for _, t := range targets {
		configName := t.Type
		if t.ResolvedType != "" && configName != t.ResolvedType && !strings.Contains(t.ResolvedType, ".") {
			// It's in the same package but an alias
			fmt.Fprintf(&body, "type %s %s\n\n", configName, t.ResolvedType)
		} else if t.PackageName == packageName && t.ResolvedType == t.Type {
			// Struct definition in same package
			fmt.Fprintf(&body, "type %s struct {\n", configName)
			for _, field := range t.StructType.Fields.List {
				if len(field.Names) != 1 || !ast.IsExported(field.Names[0].Name) {
					continue
				}
				var value bytes.Buffer
				format.Node(&value, token.NewFileSet(), field.Type)
				fmt.Fprintf(&body, "%s %s\n", field.Names[0].Name, value.String())
			}
			body.WriteString("}\n\n")
		} else if t.ResolvedType != "" && strings.Contains(t.ResolvedType, ".") {
			fmt.Fprintf(&body, "type %s %s\n\n", configName, t.ResolvedType)
		}

		optName := configName + "Option"
		fmt.Fprintf(&body, "type %s interface {\n\tapply%s(*%s)\n}\n\n", optName, configName, configName)

		fmt.Fprintf(&body, "type %sFunc func(*%s)\n", strings.ToLower(optName[:1])+optName[1:], configName)
		fmt.Fprintf(&body, "func (f %sFunc) apply%s(c *%s) {\n\tf(c)\n}\n\n", strings.ToLower(optName[:1])+optName[1:], configName, configName)

		initExpr := configName + "{}"
		if t.Default != "" {
			initExpr = t.Default
		}

		constructorName := "New" + configName
		fmt.Fprintf(&body, "func %s(options ...%s) %s {\n", constructorName, optName, configName)
		fmt.Fprintf(&body, "\tconfig := %s(%s)\n", configName, initExpr)
		fmt.Fprintf(&body, "\tfor _, option := range options {\n\t\toption.apply%s(&config)\n\t}\n", configName)
		fmt.Fprintf(&body, "\treturn config\n}\n\n")

		fmt.Fprintf(&body, "func (c %s) ResolveDefault() %s {\n\treturn %s(%s)\n}\n\n", configName, t.ResolvedType, t.ResolvedType, initExpr)
		fmt.Fprintf(&body, "func (c %s) Resolve() %s {\n\treturn %s(c)\n}\n\n", configName, t.ResolvedType, t.ResolvedType)
	}

	// Generate field options
	for _, fieldName := range fieldOrder {
		info := fieldsMap[fieldName]

		if len(info.Targets) == 1 {
			t := info.Targets[0]
			optName := t.Type + "Option"
			funcOptName := strings.ToLower(optName[:1]) + optName[1:] + "Func"

			fmt.Fprintf(&body, "func With%s(v %s) %s {\n", fieldName, info.TypeStr, optName)
			fmt.Fprintf(&body, "\treturn %s(func(c *%s) {\n", funcOptName, t.Type)
			fmt.Fprintf(&body, "\t\tc.%s = v\n", fieldName)
			fmt.Fprintf(&body, "\t})\n}\n\n")
		} else {
			sharedOptIface := fieldName + "Option"
			fmt.Fprintf(&body, "type %s interface {\n", sharedOptIface)
			for _, t := range info.Targets {
				fmt.Fprintf(&body, "\t%sOption\n", t.Type)
			}
			fmt.Fprintf(&body, "}\n\n")

			structName := strings.ToLower(fieldName[:1]) + fieldName[1:] + "Opt"
			fmt.Fprintf(&body, "type %s struct { v %s }\n", structName, info.TypeStr)

			for _, t := range info.Targets {
				fmt.Fprintf(&body, "func (o %s) apply%s(c *%s) {\n", structName, t.Type, t.Type)
				fmt.Fprintf(&body, "\tc.%s = o.v\n", fieldName)
				fmt.Fprintf(&body, "}\n")
			}
			fmt.Fprintf(&body, "\n")

			fmt.Fprintf(&body, "func With%s(v %s) %s {\n", fieldName, info.TypeStr, sharedOptIface)
			fmt.Fprintf(&body, "\treturn %s{v}\n", structName)
			fmt.Fprintf(&body, "}\n\n")
		}
	}

	// Handle Presets
	var presetTargets []*Target
	for _, t := range targets {
		if t.Preset != "" {
			presetTargets = append(presetTargets, t)
		}
	}

	if len(presetTargets) > 0 {
		if len(presetTargets) == 1 {
			t := presetTargets[0]
			optName := t.Type + "Option"
			funcOptName := strings.ToLower(optName[:1]) + optName[1:] + "Func"

			fmt.Fprintf(&body, "func WithPreset(level int) %s {\n", optName)
			fmt.Fprintf(&body, "\treturn %s(func(c *%s) {\n", funcOptName, t.Type)
			fmt.Fprintf(&body, "\t\t*c = %s(%s(level))\n", t.Type, t.Preset)
			fmt.Fprintf(&body, "\t})\n}\n\n")
		} else {
			fmt.Fprintf(&body, "type PresetOption interface {\n")
			for _, t := range presetTargets {
				fmt.Fprintf(&body, "\t%sOption\n", t.Type)
			}
			fmt.Fprintf(&body, "}\n\n")

			fmt.Fprintf(&body, "type presetOpt int\n")
			for _, t := range presetTargets {
				fmt.Fprintf(&body, "func (o presetOpt) apply%s(c *%s) {\n", t.Type, t.Type)
				fmt.Fprintf(&body, "\t*c = %s(%s(int(o)))\n", t.Type, t.Preset)
				fmt.Fprintf(&body, "}\n")
			}
			fmt.Fprintf(&body, "\nfunc WithPreset(level int) PresetOption {\n")
			fmt.Fprintf(&body, "\treturn presetOpt(level)\n")
			fmt.Fprintf(&body, "}\n\n")
		}
	}

	var sourceCode bytes.Buffer
	sourceCode.WriteString("// Code generated by config-generator. DO NOT EDIT.\n\n")
	fmt.Fprintf(&sourceCode, "package %s\n", packageName)

	// filter imports used in the generated file
	finalImports := make(map[string]string)
	for name, path := range usedImports {
		if strings.Contains(body.String(), name+".") {
			finalImports[name] = path
		}
	}

	if len(finalImports) > 0 {
		names := make([]string, 0, len(finalImports))
		for name := range finalImports {
			names = append(names, name)
		}
		sort.Strings(names)
		sourceCode.WriteString("\nimport (\n")
		for _, name := range names {
			fmt.Fprintf(&sourceCode, "\t%s \"%s\"\n", name, finalImports[name])
		}
		sourceCode.WriteString(")\n")
	}
	sourceCode.WriteString("\n")
	sourceCode.WriteString(body.String())

	formatted, err := format.Source(sourceCode.Bytes())
	if err != nil {
		fmt.Println(sourceCode.String())
		fatal("format generated source: %v", err)
	}
	if err := os.WriteFile(output, formatted, 0644); err != nil {
		fatal("write %s: %v", output, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "config-generator: "+format+"\n", args...)
	os.Exit(1)
}
