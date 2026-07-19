package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func runGenerate(goCommand, goWork string, passthroughFlags, pkgPattern []string, test bool) error {
	listArgs := append([]string{"list", "-json"}, pkgPattern...)
	listCmd := exec.Command(goCommand, listArgs...)
	listCmd.Dir = filepath.Dir(goWork)
	listCmd.Env = append(os.Environ(), "GOWORK="+goWork)

	output, err := listCmd.Output()
	if err != nil {
		return fmt.Errorf("go list failed: %w", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(output)))

	var wg sync.WaitGroup
	errCh := make(chan error, 1000)

	for {
		var pkg struct {
			Dir          string
			GoFiles      []string
			CgoFiles     []string
			TestGoFiles  []string
			XTestGoFiles []string
		}
		if err := dec.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		var regularFiles []string
		regularFiles = append(regularFiles, pkg.GoFiles...)
		regularFiles = append(regularFiles, pkg.CgoFiles...)

		if len(regularFiles) > 0 {
			wg.Add(1)
			go func(dir string, files []string) {
				defer wg.Done()
				if err := runGenerateFiles(goCommand, goWork, dir, passthroughFlags, files); err != nil {
					errCh <- err
				}
			}(pkg.Dir, regularFiles)
		}

		if test {
			if len(pkg.TestGoFiles) > 0 {
				wg.Add(1)
				go func(dir string, files []string) {
					defer wg.Done()
					if err := runGenerateFiles(goCommand, goWork, dir, passthroughFlags, files); err != nil {
						errCh <- err
					}
				}(pkg.Dir, pkg.TestGoFiles)
			}

			if len(pkg.XTestGoFiles) > 0 {
				wg.Add(1)
				go func(dir string, files []string) {
					defer wg.Done()
					if err := runGenerateFiles(goCommand, goWork, dir, passthroughFlags, files); err != nil {
						errCh <- err
					}
				}(pkg.Dir, pkg.XTestGoFiles)
			}
		}
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("generate failed with %d errors, first error: %w", len(errs), errs[0])
	}

	return nil
}

func runGenerateFiles(goCommand, goWork, dir string, passthroughFlags, files []string) error {
	args := append([]string{"generate"}, passthroughFlags...)
	args = append(args, files...)

	cmd := exec.Command(goCommand, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK="+goWork)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
