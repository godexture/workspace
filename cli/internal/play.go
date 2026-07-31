package cli

import (
	"context"
	"os"

	"github.com/godexture/godec/cli/internal/play"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/conversion"
	"github.com/spf13/cobra"
)

func newPlayCommand() *cobra.Command {
	var options playOptions
	command := &cobra.Command{
		Use:   "play INPUT",
		Short: "Play an audio file",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runPlay(command, args[0], options)
		},
	}
	command.Flags().StringVar(&options.demuxer, "demuxer", "", "Input demuxer specification (name:key=value,...)")
	command.Flags().StringVar(&options.decoder, "decoder", "", "Input decoder specification (name:key=value,...)")
	command.Flags().IntVarP(&options.jobs, "jobs", "j", 0, "Maximum parallel jobs")
	command.Flags().StringArrayVarP(&options.filters, "filter", "e", nil, "Filter specification (name:key=value,...)")
	command.Flags().StringArrayVarP(&options.inputs, "input", "i", nil, "Named auxiliary input (NAME=PATH)")
	command.Flags().StringArrayVarP(&options.wires, "wire", "w", nil, "Connect FILTER_ALIAS.PORT to SOURCE_ALIAS.PORT; @out.in sets the playback input")
	return command
}

type playOptions graphOptions

func (options playOptions) graphOptions() graphOptions { return graphOptions(options) }

func runPlay(command *cobra.Command, inputPath string, options playOptions) error {
	spec, err := buildPlaybackSpec(options.graphOptions())
	if err != nil {
		return err
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer input.Close()
	auxiliaryInputs, auxiliaryClosers, err := openAuxiliaryInputs(options.inputs)
	if err != nil {
		return err
	}
	defer closeInputs(auxiliaryClosers)

	controller := play.NewController()
	runContext, cancel := context.WithCancel(command.Context())
	defer cancel()
	built, err := conversion.BuildPlayback(runContext, conversion.InputSet{Main: input, Aux: auxiliaryInputs}, spec, controller.Sink(), pipeline.ObservationOff)
	if err != nil {
		return err
	}
	defer built.Close()
	done := make(chan error, 1)
	go func() { done <- built.Run(runContext) }()
	return play.Interact(command.InOrStdin(), command.ErrOrStderr(), controller, done, cancel)
}
