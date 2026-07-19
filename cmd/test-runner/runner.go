package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godexture/tools/internal/workspace"
)

// runTests configures and executes the go test command, parsing its output.
func runTests(goCommand, goWork string, passthroughFlags, pkgPattern []string, parallel int, simd bool) error {
	args := append([]string{"test", "-json"}, passthroughFlags...)
	if parallel > 0 && !hasFlag(args, "parallel") {
		args = append(args, "-parallel", strconv.Itoa(parallel))
	}
	args = workspace.AppendPackageArgs(args, pkgPattern)

	cmd := exec.Command(goCommand, args...)
	cmd.Dir = filepath.Dir(goWork)
	cmd.Env = goEnv(goWork, simd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	parseTestOutput(stdout)

	return cmd.Wait()
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-") == name {
			return true
		}
	}
	return false
}

func goEnv(goWork string, simd bool) []string {
	env := append(os.Environ(), "GOWORK="+goWork)
	if !simd {
		return env
	}
	return setEnv(env, "GOEXPERIMENT", enableExperiment(os.Getenv("GOEXPERIMENT"), "simd"))
}

func enableExperiment(value, experiment string) string {
	experiments := strings.Split(value, ",")
	enabled := make([]string, 0, len(experiments)+1)
	for _, current := range experiments {
		current = strings.TrimSpace(current)
		if current == "" || current == experiment || current == "no"+experiment {
			continue
		}
		enabled = append(enabled, current)
	}
	return strings.Join(append(enabled, experiment), ",")
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}
