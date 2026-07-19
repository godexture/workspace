package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/workspace"
)

func main() {
	scriptArgs, testArgs := workspace.SplitArgs(os.Args[1:])

	var workPath string
	var goCommand string
	var parallel int
	var simd bool

	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ExitOnError)
	flags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	flags.StringVar(&goCommand, "go", "go", "go command to run")
	flags.IntVar(&parallel, "parallel", 0, "maximum concurrent tests in each test binary; 0 uses Go's default")
	flags.BoolVar(&simd, "simd", false, "enable GOEXPERIMENT=simd for the test build")
	if err := flags.Parse(scriptArgs); err != nil {
		cli.Fatal(err)
	}

	goWork, passthroughFlags, pkgPattern, err := workspace.SetupCLI(testArgs, goCommand, workPath, flagNeedsValue)
	if err != nil {
		cli.Fatal(err)
	}

	err = runTests(goCommand, goWork, passthroughFlags, pkgPattern, parallel, simd)
	if err != nil {
		os.Exit(1)
	}
}

func flagNeedsValue(flag string) bool {
	name := strings.TrimLeft(strings.SplitN(flag, "=", 2)[0], "-")
	switch name {
	case "benchtime", "blockprofile", "blockprofilerate", "covermode", "coverpkg",
		"count", "cpu", "cpuprofile", "exec", "fuzz", "fuzztime", "list",
		"memprofile", "memprofilerate", "mutexprofile", "mutexprofilefraction",
		"o", "outputdir", "parallel", "run", "shuffle", "skip", "timeout",
		"trace", "vet":
		return true
	default:
		return false
	}
}
