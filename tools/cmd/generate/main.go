package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/godexture/tools/internal/cli"
	"github.com/godexture/tools/internal/workspace"
	"github.com/spf13/pflag"
)

func main() {
	log.Printf("starting generate")
	scriptArgs, generateArgs := workspace.SplitArgs(os.Args[1:])

	var workPath string
	var goCommand string
	var test bool

	flags := pflag.NewFlagSet(filepath.Base(os.Args[0]), pflag.ExitOnError)
	flags.StringVar(&workPath, "work", "", "path to go.work; defaults to searching from the current directory")
	flags.StringVar(&goCommand, "go", "go", "go command to run")
	flags.BoolVar(&test, "test", false, "include test files")
	if err := flags.Parse(scriptArgs); err != nil {
		cli.Fatal(err)
	}

	goWork, passthroughFlags, pkgPattern, err := workspace.SetupCLI(generateArgs, goCommand, workPath, flagNeedsValue)
	if err != nil {
		cli.Fatal(err)
	}

	tmpDir, err := os.MkdirTemp("", "godexture-gen-*")
	if err != nil {
		cli.Fatal(fmt.Errorf("failed to create temp dir: %w", err))
	}
	defer os.RemoveAll(tmpDir)

	if err := buildTools(goCommand, goWork, tmpDir); err != nil {
		cli.Fatal(err)
	}

	err = runGenerate(goCommand, goWork, passthroughFlags, pkgPattern, test)
	if err != nil {
		os.Exit(1)
	}
	log.Printf("finished generate")
}

func flagNeedsValue(flag string) bool {
	name := strings.TrimLeft(strings.SplitN(flag, "=", 2)[0], "-")
	switch name {
	case "run", "tags", "asmflags", "buildmode", "compiler", "gccgoflags", "gcflags", "installsuffix", "ldflags", "mod", "modfile", "overlay", "p", "pkgdir", "toolexec":
		return true
	default:
		return false
	}
}
