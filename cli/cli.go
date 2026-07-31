package cli

import (
	"context"

	command "github.com/godexture/godec/cli/internal"
)

func Execute(ctx context.Context, args []string) error {
	return command.Execute(ctx, args)
}
