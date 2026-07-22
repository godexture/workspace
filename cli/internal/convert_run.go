package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	godec "github.com/godexture/core"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/routing"
	"github.com/spf13/cobra"
)

func runConvert(command *cobra.Command, inputPath, outputPath string, options convertOptions) (resultErr error) {
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

	inputFile, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer inputFile.Close()

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
	var conversion *pipeline.Pipeline

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
			if conversion != nil {
				report.Pipeline = conversion.Snapshot()
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

	negotiationStarted := time.Now()
	muxer, muxValues, err := selectMuxer(options.format, outputPath)
	if err != nil {
		return err
	}
	muxConfig, err := configureManifest("format", muxer, muxValues)
	if err != nil {
		return err
	}
	targetCodec, codecValues, err := resolveCodec(options.codec, muxer.DefaultCodec)
	if err != nil {
		return err
	}
	if !muxer.Supports(targetCodec) {
		return fmt.Errorf("format %q does not support codec %q", muxer.Name, targetCodec)
	}
	encoder, err := godec.NewResolver().NewEncoderResolver(godec.DefaultEncoderRegistry).ResolveEncoder(targetCodec)
	if err != nil {
		return err
	}
	encoderConfig, err := configureManifest("codec", encoder, codecValues)
	if err != nil {
		return err
	}
	demuxManifest, demuxConfig, err := resolvePlugin("demuxer", options.demuxer, godec.DefaultDemuxerRegistry)
	if err != nil {
		return err
	}
	decoderManifest, decodeConfig, err := resolvePlugin("decoder", options.decoder, godec.DefaultDecoderRegistry)
	if err != nil {
		return err
	}
	filterSpecs, err := resolveFilters(options.filters)
	if err != nil {
		return err
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

	geometry, err := godec.NewNegotiator().NegotiateConversion(command.Context(), routing.ConversionSpec{
		Input: input, Output: output,
		DemuxManifest: demuxManifest, DemuxConfig: demuxConfig,
		DecoderManifest: decoderManifest, DecodeConfig: decodeConfig,
		Filters: filterSpecs, TargetCodec: targetCodec, EncodeConfig: encoderConfig, MuxConfig: muxConfig,
		Resources: registry.ResourceBudget{Parallelism: options.jobs},
	})
	report.Phases.Negotiation = time.Since(negotiationStarted)
	if err != nil {
		return err
	}

	observation := pipeline.ObservationOff
	if options.metrics {
		observation = pipeline.ObservationMetrics
	} else if progress.enabled {
		observation = pipeline.ObservationProgress
	}
	buildStarted := time.Now()
	conversion, err = godec.NewBuilder().Build(geometry, pipeline.WithObservation(observation))
	report.Phases.Build = time.Since(buildStarted)
	if err != nil {
		_ = geometry.Close()
		return err
	}
	defer conversion.Close()

	if options.dryRun {
		return writePipelineDescription(command.OutOrStdout(), conversion.Description())
	}
	description := conversion.Description()
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
		progressReporter = startProgressReporter(command.ErrOrStderr(), conversion, measuredInput, progress)
	}
	executionStarted := time.Now()
	err = conversion.Run(command.Context())
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
