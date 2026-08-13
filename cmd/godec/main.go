package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/godexture/godec/cli"
	"github.com/godexture/godec/standard"
)

func main() {
	instance, err := standard.NewHost()
	if err != nil {
		fmt.Fprintf(os.Stderr, "godec: %v\n", err)
		os.Exit(int(cli.ExitRuntime))
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// The command owns its own rendering, including of its failures. This
	// wrapper only turns the classification into a process exit; printing the
	// joined error here would repeat what the user already read.
	code := cli.Run(ctx, instance, os.Args[1:]).Code
	cancel()
	os.Exit(int(code))
}
