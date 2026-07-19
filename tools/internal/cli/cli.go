package cli

import (
	"fmt"
	"os"
)

// Fatal prints the error message and exits with status 1.
func Fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

// Fatalf formats an error message, prints it, and exits with status 1.
func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
