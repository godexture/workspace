package main

import (
	"context"
	"os"
	"os/signal"

	cli "github.com/godexture/cli/internal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli.Execute(ctx, os.Args[1:]); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
