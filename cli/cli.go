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

const cliEventLimit = 64

// ExitCode classifies command completion without inspecting error text.
type ExitCode int

const (
	// ExitSuccess reports a completed conversion or preview.
	ExitSuccess ExitCode = 0
	// ExitUsage reports invalid command arguments or an unsafe same-file request.
	ExitUsage ExitCode = 2
	// ExitPlanning reports a request that could not be prepared.
	ExitPlanning ExitCode = 3
	// ExitRuntime reports execution or rendering failure.
	ExitRuntime ExitCode = 4
	// ExitCanceled reports context cancellation or deadline expiry.
	ExitCanceled ExitCode = 130
)

// Option configures an embedded CLI invocation.
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
		return requestExit(err)
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
		host.RetainEvents(cliEventLimit),
		host.DeliverEvents(cliEventLimit, renderer),
	))
	observationLost, renderErr := renderer.finish(result.Events, result.Observation)
	if runErr == nil {
		runErr = renderErr
	}
	closeErr := prepared.Close()
	if runErr == nil {
		runErr = closeErr
	}
	if err := renderResult(configuration.stdout, configuration.stderr, result, runErr, observationLost); err != nil && runErr == nil {
		runErr = err
	}
	if runErr != nil {
		return runtimeExit(ctx, runErr)
	}
	return ExitSuccess
}
