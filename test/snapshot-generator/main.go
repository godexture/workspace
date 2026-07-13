package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/godexture/codec-flac/test/config"
	"github.com/godexture/sdk/testutil"
)

func main() {
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup

	testFiles := config.EnumerateTestdataFiles()
	errChan := make(chan error, len(testFiles))

	for _, fileName := range testFiles {
		wg.Add(1)
		go func(fileName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := generateSnapshot(fileName); err != nil {
				errChan <- fmt.Errorf("failed to generate snapshot for %s: %w", fileName, err)
			}
		}(fileName)
	}

	wg.Wait()
	close(errChan)

	hasError := false
	for err := range errChan {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		hasError = true
	}

	if hasError {
		os.Exit(1)
	}
}

func generateSnapshot(fileName string) error {
	dataPath := config.BuildTestdataPath(fileName)
	file, err := os.Open(dataPath)
	if err != nil {
		return fmt.Errorf("failed to open test FLAC file: %w", err)
	}
	defer file.Close()

	decodedPCM, err := testutil.DecodeWithFFmpeg(file)
	if err != nil {
		return fmt.Errorf("failed to decode %s with FFmpeg: %w", fileName, err)
	}

	if err := testutil.SaveSnapshot(config.BuildSnapshotPath(fileName), decodedPCM); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	fmt.Printf("Generated snapshot for %s (%d samples)\n", fileName, len(decodedPCM))
	return nil
}
