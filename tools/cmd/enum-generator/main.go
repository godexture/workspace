package main

import (
	"os"
	"strings"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/enum-generator/generator"
	"github.com/godexture/tools/internal/enumscan"
	"github.com/spf13/pflag"
)

func main() {
	outputFlag := pflag.String("output", "", "generated file name (default: {GOFILE}_enum.go)")
	pflag.Parse()

	output := *outputFlag
	if output == "" {
		gofile := os.Getenv("GOFILE")
		if gofile != "" {
			output = strings.TrimSuffix(gofile, ".go") + "_enum.go"
		} else {
			output = "config_enum.go"
		}
	}

	allFiles, _, packageName, err := enumscan.LoadPackage(".", output)
	if err != nil {
		cli.Fatalf("parse package: %v", err)
	}

	enums := enumscan.DiscoverStringEnums(allFiles, packageName)
	if len(enums) == 0 {
		cli.Fatalf("no string-backed enum types with const values found in package %s", packageName)
	}

	generator.Generate(output, packageName, enums)
}
