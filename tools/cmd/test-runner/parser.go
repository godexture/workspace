package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// parseTestOutput reads go test -json output and formats it to stdout.
func parseTestOutput(stdout io.Reader) {
	packageOutput := make(map[string]string)
	testOutput := make(map[string]map[string]string)
	packageHasFailedTests := make(map[string]bool)

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event testEvent
		if json.Unmarshal(line, &event) != nil {
			fmt.Println(string(line))
			continue
		}
		if event.Package == "" {
			continue
		}

		if event.Output != "" {
			packageOutput[event.Package] += event.Output
			if event.Test != "" {
				if testOutput[event.Package] == nil {
					testOutput[event.Package] = make(map[string]string)
				}
				testOutput[event.Package][event.Test] += event.Output
			}
		}

		switch event.Action {
		case "fail":
			if event.Test != "" {
				packageHasFailedTests[event.Package] = true
				fmt.Printf("[FAIL] %s: %s (%.4fs)\n", event.Package, event.Test, event.Elapsed)
				if out := testOutput[event.Package][event.Test]; out != "" {
					fmt.Println(strings.TrimRight(out, "\r\n"))
					fmt.Println()
				}
			} else {
				if !packageHasFailedTests[event.Package] {
					fmt.Printf("[FAIL] %s (%.4fs)\n", event.Package, event.Elapsed)
					if out := packageOutput[event.Package]; out != "" {
						fmt.Println(strings.TrimRight(out, "\r\n"))
						fmt.Println()
					}
				}
			}
		case "pass":
			if event.Test == "" {
				fmt.Printf("[PASS] %s (%.4fs)\n", event.Package, event.Elapsed)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scanner error: %v\n", err)
	}
}
