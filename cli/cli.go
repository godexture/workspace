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

// Result is what one command produced. Code is the classification a process
// wrapper needs; Err carries every independent failure behind it, including
// the ones a non-zero code cannot distinguish, such as a broken stdout, a
// cleanup failure, and the runtime failure that preceded both.
type Result struct {
	Code ExitCode
	Err  error
}

// Succeeded reports whether the command finished with nothing to report.
func (r Result) Succeeded() bool { return r.Code == ExitSuccess && r.Err == nil }

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
func Run(ctx context.Context, instance *host.Host, args []string, values ...Option) Result {
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
		return Result{Code: ExitUsage, Err: errors.New("cli requires both output streams")}
	}
	return execute(ctx, instance, args, configuration)
}

// outcome collects the failures of one command. Every stage adds what it saw
// and nothing overwrites an earlier failure, so a rendering failure during
// cleanup cannot hide the runtime failure that caused it.
type outcome struct {
	code     ExitCode
	classify func(error) ExitCode
	problems []error
}

func (o *outcome) add(err error) {
	if err != nil {
		o.problems = append(o.problems, err)
	}
}

func (o *outcome) result() Result {
	err := errors.Join(o.problems...)
	code := o.code
	if err != nil && o.classify != nil {
		code = o.classify(err)
	}
	return Result{Code: code, Err: err}
}

func execute(ctx context.Context, instance *host.Host, args []string, configuration options) Result {
	result := outcome{code: ExitSuccess}
	invocation, err := parse(args)
	if errors.Is(err, errHelp) {
		_, writeErr := io.WriteString(configuration.stdout, usage)
		result.classify = func(error) ExitCode { return ExitRuntime }
		result.add(writeErr)
		return result.result()
	}
	if err != nil {
		result.code = ExitUsage
		result.add(err)
		result.add(renderUsageError(configuration.stderr, err))
		return result.result()
	}
	request, err := invocation.request()
	if err != nil {
		result.code = requestExit(err)
		result.add(err)
		result.add(renderError(configuration.stderr, err))
		return result.result()
	}

	if invocation.planOnly {
		result.classify = func(err error) ExitCode { return planningExit(ctx, err) }
		selected, planErr := instance.Plan(ctx, request)
		if planErr != nil {
			result.add(planErr)
			result.add(renderError(configuration.stderr, planErr))
			return result.result()
		}
		result.classify = func(error) ExitCode { return ExitRuntime }
		result.add(renderPlan(configuration.stdout, selected))
		return result.result()
	}

	result.classify = func(err error) ExitCode { return planningExit(ctx, err) }
	prepared, err := instance.Prepare(ctx, request)
	if err != nil {
		result.add(err)
		result.add(renderError(configuration.stderr, err))
		return result.result()
	}
	result.classify = func(err error) ExitCode { return runtimeExit(ctx, err) }
	if renderErr := renderPlan(configuration.stdout, prepared.Plan()); renderErr != nil {
		result.add(renderErr)
		result.add(prepared.Close())
		return result.result()
	}

	renderer := &eventRenderer{destination: configuration.stderr}
	value, runErr := prepared.Run(ctx, host.Observe(
		host.ObservationDetailed,
		host.RetainEvents(cliEventLimit),
		host.DeliverEvents(cliEventLimit, renderer),
	))
	observationLost, renderErr := renderer.finish(value.Events, value.Observation)
	result.add(runErr)
	result.add(renderErr)
	// A renderer still delivering events owns stderr, so writing the result
	// summary there would interleave with it.
	if !errors.Is(renderErr, errEventRendererActive) {
		result.add(renderResult(configuration.stdout, configuration.stderr, value, runErr, observationLost))
	}
	result.add(prepared.Close())
	return result.result()
}
