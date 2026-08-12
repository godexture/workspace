// Package cli implements the official command-line surface as a client of an
// injected Host. It owns no plugin composition, catalog, planner, or runtime.
package cli

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/godexture/godec/host"
)

type ExitCode int

const (
	ExitSuccess  ExitCode = 0
	ExitUsage    ExitCode = 2
	ExitPlanning ExitCode = 3
	ExitRuntime  ExitCode = 4
	ExitCanceled ExitCode = 130
)

type Option func(*options)

type options struct {
	stdout io.Writer
	stderr io.Writer
}

// WithStreams replaces the process streams for embedding and tests.
func WithStreams(stdout, stderr io.Writer) Option {
	return func(options *options) {
		options.stdout = stdout
		options.stderr = stderr
	}
}

// Run parses and executes one command against the supplied immutable Host.
func Run(ctx context.Context, instance *host.Host, args []string, values ...Option) ExitCode {
	if ctx == nil {
		ctx = context.Background()
	}
	configuration := options{stdout: os.Stdout, stderr: os.Stderr}
	for _, option := range values {
		if option != nil {
			option(&configuration)
		}
	}
	if configuration.stdout == nil || configuration.stderr == nil {
		return ExitUsage
	}
	invocation, err := parse(args)
	if errors.Is(err, errHelp) {
		if _, writeErr := io.WriteString(configuration.stdout, usage); writeErr != nil {
			return ExitRuntime
		}
		return ExitSuccess
	}
	if err != nil {
		renderUsageError(configuration.stderr, err)
		return ExitUsage
	}
	request, err := invocation.request()
	if err != nil {
		renderError(configuration.stderr, err)
		return ExitUsage
	}
	if invocation.planOnly {
		selected, planErr := instance.Plan(ctx, request)
		if planErr != nil {
			renderError(configuration.stderr, planErr)
			return planningExit(ctx, planErr)
		}
		if err := renderPlan(configuration.stdout, selected); err != nil {
			return ExitRuntime
		}
		return ExitSuccess
	}

	prepared, err := instance.Prepare(ctx, request)
	if err != nil {
		renderError(configuration.stderr, err)
		return planningExit(ctx, err)
	}
	if err := renderPlan(configuration.stdout, prepared.Plan()); err != nil {
		_ = prepared.Close()
		return ExitRuntime
	}
	renderer := &eventRenderer{destination: configuration.stderr}
	result, runErr := prepared.Run(ctx, host.Observe(
		host.ObservationDetailed,
		host.DeliverEvents(8, renderer),
	))
	closeErr := prepared.Close()
	if runErr == nil {
		runErr = closeErr
	}
	if err := renderResult(configuration.stdout, configuration.stderr, result, runErr); err != nil && runErr == nil {
		runErr = err
	}
	if runErr != nil {
		return runtimeExit(ctx, runErr)
	}
	return ExitSuccess
}
