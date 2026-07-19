package main

import (
	"flag"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/config-generator/generator"
	"github.com/godexture/tools/internal/config-generator/parser"
	"github.com/godexture/tools/internal/config-generator/types"
)

type stringSlice []string

func (i *stringSlice) String() string { return strings.Join(*i, ",") }
func (i *stringSlice) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	var targetsFlag stringSlice
	flag.Var(&targetsFlag, "target", "target configurations (e.g., EncoderConfig,default=...)")
	outputFlag := flag.String("output", "", "generated file name (default: {GOFILE}_options.go)")

	flag.Parse()

	if len(targetsFlag) == 0 {
		cli.Fatalf("at least one -target is required")
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

	targets := make([]*types.Target, 0, len(targetsFlag))
	for _, s := range targetsFlag {
		targets = append(targets, parser.ParseTarget(s))
	}

	fileSet := token.NewFileSet()

	// Parse current package to get the output package name
	packages, err := goparser.ParseDir(fileSet, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go") && info.Name() != output
	}, goparser.ParseComments)
	if err != nil {
		cli.Fatalf("parse package: %v", err)
	}
	if len(packages) != 1 {
		cli.Fatalf("expected one package in current directory, found %d", len(packages))
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
		f, err := goparser.ParseFile(fileSet, path, nil, goparser.ParseComments)
		if err == nil {
			allFiles = append(allFiles, f)
			filePaths[f] = path
		}
		return nil
	})

	for _, t := range targets {
		parser.FindTargetInfo(t, allFiles, filePaths, outputPackageName)
	}

	generator.Generate(output, outputPackageName, targets)
}
