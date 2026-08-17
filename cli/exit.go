package cli

import (
	"context"
	"errors"

	"github.com/godexture/godec/host"
)

type planningRequestError struct {
	cause error
}

func (e planningRequestError) Error() string { return e.cause.Error() }

func (e planningRequestError) Unwrap() error { return e.cause }

func requestExit(err error) ExitCode {
	var planning planningRequestError
	if errors.As(err, &planning) {
		return ExitPlanning
	}
	return ExitUsage
}

func planningExit(ctx context.Context, _ error) ExitCode {
	if canceled(ctx) {
		return ExitCanceled
	}
	return ExitPlanning
}

func runtimeExit(ctx context.Context, err error) ExitCode {
	if canceled(ctx) {
		return ExitCanceled
	}
	var failure *host.Failure
	if errors.As(err, &failure) && failure != nil && (failure.Phase == host.PreparePhase || failure.Phase == host.ResourcePhase) {
		return ExitPlanning
	}
	return ExitRuntime
}

func canceled(ctx context.Context) bool {
	return ctx != nil && ctx.Err() != nil
}
