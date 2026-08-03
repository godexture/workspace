package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	root := flag.String("root", "docs", "directory containing Markdown documents")
	flag.Parse()

	issues, err := Check(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, issue := range issues {
		fmt.Fprintln(os.Stderr, issue)
	}
	if len(issues) != 0 {
		os.Exit(1)
	}
}
