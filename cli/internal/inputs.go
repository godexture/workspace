package cli

import (
	"fmt"
	"io"
	"os"
)

func openAuxiliaryInputs(values []string) (map[string]io.ReadSeeker, []io.Closer, error) {
	inputs := make(map[string]io.ReadSeeker, len(values))
	closers := make([]io.Closer, 0, len(values))
	for _, value := range values {
		name, path, err := parseNamedValue(value)
		if err != nil {
			closeInputs(closers)
			return nil, nil, fmt.Errorf("input: %w", err)
		}
		file, err := os.Open(path)
		if err != nil {
			closeInputs(closers)
			return nil, nil, fmt.Errorf("input %q: %w", name, err)
		}
		inputs[name] = file
		closers = append(closers, file)
	}
	return inputs, closers, nil
}

func closeInputs(inputs []io.Closer) {
	for _, input := range inputs {
		_ = input.Close()
	}
}
