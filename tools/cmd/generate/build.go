package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// buildTools compiles the necessary generators into a temporary directory
// and prepends that directory to the PATH.
func buildTools(goCommand, goWork, tmpDir string) error {
	var buildWg sync.WaitGroup
	buildErrs := make(chan error, 3)

	buildWg.Add(1)
	go func() {
		defer buildWg.Done()
		log.Printf("building config-generator...")
		buildConfigCmd := exec.Command(goCommand, "build", "-o", filepath.Join(tmpDir, "config-generator.exe"), "github.com/godexture/godec/tools/cmd/config-generator")
		buildConfigCmd.Env = append(os.Environ(), "GOWORK="+goWork)
		buildConfigCmd.Stdout = os.Stdout
		buildConfigCmd.Stderr = os.Stderr
		if err := buildConfigCmd.Run(); err != nil {
			buildErrs <- fmt.Errorf("failed to build config-generator: %w", err)
			return
		}
		log.Printf("built config-generator to %s", tmpDir)
	}()

	buildWg.Add(1)
	go func() {
		defer buildWg.Done()
		log.Printf("building enum-generator...")
		buildEnumCmd := exec.Command(goCommand, "build", "-o", filepath.Join(tmpDir, "enum-generator.exe"), "github.com/godexture/godec/tools/cmd/enum-generator")
		buildEnumCmd.Env = append(os.Environ(), "GOWORK="+goWork)
		buildEnumCmd.Stdout = os.Stdout
		buildEnumCmd.Stderr = os.Stderr
		if err := buildEnumCmd.Run(); err != nil {
			buildErrs <- fmt.Errorf("failed to build enum-generator: %w", err)
			return
		}
		log.Printf("built enum-generator to %s", tmpDir)
	}()

	buildWg.Add(1)
	go func() {
		defer buildWg.Done()
		log.Printf("pre-warming build cache for table-generator...")
		buildTableCmd := exec.Command(goCommand, "build", "github.com/godexture/godec/tools/pkg/table-generator")
		buildTableCmd.Env = append(os.Environ(), "GOWORK="+goWork)
		buildTableCmd.Stdout = os.Stdout
		buildTableCmd.Stderr = os.Stderr
		if err := buildTableCmd.Run(); err != nil {
			buildErrs <- fmt.Errorf("failed to build table-generator: %w", err)
			return
		}
		log.Printf("pre-warmed build cache for table-generator")
	}()

	buildWg.Wait()
	close(buildErrs)
	for err := range buildErrs {
		return err // return first error
	}

	originalPath := os.Getenv("PATH")
	newPath := tmpDir + string(os.PathListSeparator) + originalPath
	if err := os.Setenv("PATH", newPath); err != nil {
		return fmt.Errorf("failed to set PATH: %w", err)
	}

	return nil
}
