package audio

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadSnapshot loads float32 samples from a text snapshot file (one sample per line).
func LoadSnapshot(path string) ([]float32, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var pcm []float32
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parsedValue, err := strconv.ParseFloat(line, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse float at index %d: %w", len(pcm), err)
		}

		pcm = append(pcm, float32(parsedValue))
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return pcm, nil
}

// SaveSnapshot writes float32 samples to a text snapshot file (one sample per line).
func SaveSnapshot(path string, pcm []float32) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, val := range pcm {
		if _, err := fmt.Fprintf(writer, "%f\n", val); err != nil {
			return fmt.Errorf("failed to write PCM sample: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush buffer: %w", err)
	}

	return nil
}
