package cli

import (
	"context"
	"errors"

	"github.com/godexture/godec/host"
)

func planningExit(ctx context.Context, err error) ExitCode {
	if canceled(ctx, err) {
		return ExitCanceled
	}
	return ExitPlanning
}

func runtimeExit(ctx context.Context, err error) ExitCode {
	if canceled(ctx, err) {
		return ExitCanceled
	}
	var failure *host.Failure
	if errors.As(err, &failure) && failure != nil && (failure.Phase == host.PreparePhase || failure.Phase == host.ResourcePhase) {
		return ExitPlanning
	}
	return ExitRuntime
}

func canceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx != nil && ctx.Err() != nil
}
