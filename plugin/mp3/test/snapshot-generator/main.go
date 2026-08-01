package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/godexture/godec/plugin/mp3/test/config"
	"github.com/godexture/godec/plugin/mp3/test/minimp3"
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

	data, err := os.ReadFile(dataPath)
	if err != nil {
		return fmt.Errorf("failed to read test MP3 file: %w", err)
	}

	f, err := os.Create(config.BuildSnapshotPath(fileName))
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	pcmSlice := minimp3.Decode(data)
	for _, val := range pcmSlice {
		if _, err := fmt.Fprintf(writer, "%f\n", val); err != nil {
			return fmt.Errorf("failed to write PCM sample: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush buffer: %w", err)
	}

	fmt.Printf("Generated snapshot for %s (%d samples)\n", fileName, len(pcmSlice))

	return nil
}
