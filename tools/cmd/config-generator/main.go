package main

import (
	"os"
	"strings"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/config-generator/generator"
	"github.com/godexture/tools/internal/config-generator/parser"
	"github.com/godexture/tools/internal/config-generator/types"
	"github.com/godexture/tools/internal/enumscan"
	"github.com/spf13/pflag"
)

type stringSlice []string

func (i *stringSlice) String() string { return strings.Join(*i, ",") }
func (*stringSlice) Type() string     { return "string" }
func (i *stringSlice) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	var targetsFlag stringSlice
	pflag.Var(&targetsFlag, "target", "target configurations (e.g., EncoderConfig,default=...)")
	outputFlag := pflag.String("output", "", "generated file name (default: {GOFILE}_options.go)")

	pflag.Parse()

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

	allFiles, filePaths, outputPackageName, err := enumscan.LoadPackage(".", output)
	if err != nil {
		cli.Fatalf("parse package: %v", err)
	}

	for _, t := range targets {
		parser.FindTargetInfo(t, allFiles, filePaths, outputPackageName)
	}

	generator.Generate(output, outputPackageName, targets)
}
