package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/pprof"
	"time"

	"github.com/godexture/core/pipeline"
	"github.com/godexture/sdk/catalog"
	"github.com/godexture/sdk/conversion"
	"github.com/godexture/sdk/profiling"
	"github.com/spf13/cobra"
)

func runConvert(command *cobra.Command, inputPath, outputPath string, options convertOptions) (resultErr error) {
	if pprofPath := os.Getenv("GODEC_PPROF"); pprofPath != "" {
		if err := profiling.RejectPathCollision(pprofPath, inputPath, outputPath); err != nil {
			return err
		}
		f, err := os.Create(pprofPath)
		if err != nil {
			return fmt.Errorf("failed to create pprof file: %w", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("failed to start pprof: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if options.dryRun && options.metrics {
		return errors.New("--dry-run and --metrics cannot be used together")
	}
	verbose, err := command.Flags().GetBool("verbose")
	if err != nil {
		return err
	}
	progress, err := resolveProgressConfig(options.progress, isTerminalWriter(command.ErrOrStderr()), options.dryRun)
	if err != nil {
		return err
	}

	spec, err := buildSpec(options, outputPath, catalog.Build().Outputs)
	if err != nil {
		return err
	}

	inputFile, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer inputFile.Close()
	auxiliaryInputs := make(map[string]io.ReadSeeker, len(options.inputs))
	for _, value := range options.inputs {
		name, path, parseErr := parseNamedValue(value)
		if parseErr != nil {
			return fmt.Errorf("input: %w", parseErr)
		}
		auxiliaryFile, openErr := os.Open(path)
		if openErr != nil {
			return fmt.Errorf("input %q: %w", name, openErr)
		}
		defer auxiliaryFile.Close()
		auxiliaryInputs[name] = auxiliaryFile
	}

	inputInfo, err := inputFile.Stat()
	if err != nil {
		return err
	}

	totalStarted := time.Now()
	conversionSucceeded := false
	defer func() {
		if conversionSucceeded {
			if _, writeErr := fmt.Fprintln(command.ErrOrStderr(), "Conversion completed successfully."); writeErr != nil {
				resultErr = errors.Join(resultErr, writeErr)
			}
		}
	}()
	var report metricsReport
	var sampler *runtimeSampler
	var measuredInput *measuredReadSeeker
	var measuredOutput *measuredWriter
	var built *pipeline.Pipeline

	if options.metrics {
		sampler = startRuntimeSampler(500 * time.Millisecond)
		defer func() {
			report.Err = resultErr
			report.Phases.Total = time.Since(totalStarted)
			if measuredInput != nil {
				report.Input = measuredInput.Snapshot()
			}
			if measuredOutput != nil {
				report.Output = measuredOutput.Snapshot()
			}
			if built != nil {
				report.Pipeline = built.Snapshot()
			}
			report.Runtime = sampler.Stop()
			if writeErr := writeMetricsReport(command.ErrOrStderr(), report); writeErr != nil {
				resultErr = errors.Join(resultErr, writeErr)
			}
		}()
	}

	var input io.ReadSeeker = inputFile
	if progress.enabled || options.metrics || (verbose && !options.dryRun) {
		measuredInput = newMeasuredReadSeeker(inputFile, inputInfo.Size())
		input = measuredInput
	}

	var pending *pendingOutput
	var output io.Writer = io.Discard
	if !options.dryRun {
		var skip bool
		pending, skip, err = prepareOutput(command, outputPath, options.force)
		if err != nil || skip {
			return err
		}
		defer pending.abort()
		output = pending.file
	}
	if options.metrics || (verbose && !options.dryRun) {
		measuredOutput = newMeasuredWriter(output)
		output = measuredOutput
	}

	observation := pipeline.ObservationOff
	if options.metrics {
		observation = pipeline.ObservationMetrics
	} else if progress.enabled {
		observation = pipeline.ObservationProgress
	}

	negotiationStarted := time.Now()
	built, err = conversion.Build(command.Context(), conversion.InputSet{Main: input, Aux: auxiliaryInputs}, output, spec, observation)
	report.Phases.Negotiation = time.Since(negotiationStarted)
	if err != nil {
		return err
	}
	defer built.Close()

	if options.dryRun {
		return writePipelineDescription(command.OutOrStdout(), built.Description())
	}
	description := built.Description()
	if err := writeConversionStart(command.ErrOrStderr(), description); err != nil {
		return err
	}
	if verbose {
		if err := writePipelineDescription(command.ErrOrStderr(), description); err != nil {
			return err
		}
	}

	var progressReporter *progressReporter
	if progress.enabled {
		progressReporter = startProgressReporter(command.ErrOrStderr(), built, measuredInput, progress)
	}
	executionStarted := time.Now()
	err = built.Run(command.Context())
	report.Phases.Execution = time.Since(executionStarted)
	if progressReporter != nil {
		progressReporter.Stop(err == nil)
	}
	if err != nil {
		return err
	}

	finalizeStarted := time.Now()
	err = pending.commit()
	report.Phases.Finalize = time.Since(finalizeStarted)
	if err != nil {
		return err
	}
	conversionSucceeded = true
	if verbose && !options.metrics {
		inputSnapshot := measuredInput.Snapshot()
		outputSnapshot := measuredOutput.Snapshot()
		_, err = fmt.Fprintf(command.ErrOrStderr(), "Summary: elapsed=%s read=%s wrote=%s\n",
			formatMetricDuration(report.Phases.Execution), formatBytes(inputSnapshot.BytesRead), formatBytes(outputSnapshot.BytesWritten))
		return err
	}
	return nil
}

