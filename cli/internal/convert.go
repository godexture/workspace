package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/godexture/sdk/catalog"
	"github.com/godexture/sdk/cliflag"
	"github.com/godexture/sdk/conversion"
	"github.com/spf13/cobra"
)

func newConvertCommand() *cobra.Command {
	var options convertOptions
	command := &cobra.Command{
		Use:  "convert INPUT OUTPUT",
		Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runConvert(command, args[0], args[1], options)
		},
	}
	command.Flags().StringVar(&options.format, "format", "", "Output format specification (name:key=value,...)")
	command.Flags().StringVar(&options.codec, "codec", "", "Output codec specification (name:key=value,...)")
	command.Flags().StringVar(&options.demuxer, "demuxer", "", "Input demuxer specification (name:key=value,...)")
	command.Flags().StringVar(&options.decoder, "decoder", "", "Input decoder specification (name:key=value,...)")
	command.Flags().IntVarP(&options.jobs, "jobs", "j", 0, "Maximum parallel jobs")
	command.Flags().BoolVar(&options.force, "force", false, "Overwrite an existing output file")
	command.Flags().StringArrayVar(&options.filters, "filter", nil, "Filter specification (name:key=value,...)")
	command.Flags().StringVar(&options.progress, "progress", "auto", "Progress display: auto, always, or never")
	command.Flags().BoolVar(&options.metrics, "metrics", false, "Report conversion and runtime metrics")
	command.Flags().BoolVar(&options.dryRun, "dry-run", false, "Resolve and validate the pipeline without converting")
	return command
}

type convertOptions struct {
	format   string
	codec    string
	demuxer  string
	decoder  string
	progress string
	jobs     int
	force    bool
	metrics  bool
	dryRun   bool
	filters  []string
}

// buildSpec turns convert flags into a conversion.Spec. --format infers the
// muxer from the output extension (via the catalog) when omitted; --codec
// carries both the target codec ID and the values applied to whichever
// encoder is resolved for it (CLI has no separate flag to pick an encoder
// plugin by name).
func buildSpec(options convertOptions, outputPath string, outputs []catalog.OutputFormat) (conversion.Spec, error) {
	format, err := parsePluginSpec(options.format)
	if err != nil {
		return conversion.Spec{}, fmt.Errorf("format: %w", err)
	}
	if format == nil {
		name, inferErr := inferMuxerName(outputs, outputPath)
		if inferErr != nil {
			return conversion.Spec{}, inferErr
		}
		format = &conversion.PluginSpec{Name: name}
	}

	codec, err := parsePluginSpec(options.codec)
	if err != nil {
		return conversion.Spec{}, fmt.Errorf("codec: %w", err)
	}

	spec := conversion.Spec{Muxer: *format, Parallelism: options.jobs}
	if codec != nil {
		spec.Codec = codec.Name
		spec.Encoder = &conversion.PluginSpec{Values: codec.Values}
	}
	if spec.Demuxer, err = parsePluginSpec(options.demuxer); err != nil {
		return conversion.Spec{}, fmt.Errorf("demuxer: %w", err)
	}
	if spec.Decoder, err = parsePluginSpec(options.decoder); err != nil {
		return conversion.Spec{}, fmt.Errorf("decoder: %w", err)
	}
	for _, value := range options.filters {
		filter, parseErr := parsePluginSpec(value)
		if parseErr != nil {
			return conversion.Spec{}, fmt.Errorf("filter: %w", parseErr)
		}
		spec.Filters = append(spec.Filters, conversion.FilterSpec{PluginSpec: *filter})
	}
	return spec, nil
}

func parsePluginSpec(value string) (*conversion.PluginSpec, error) {
	if value == "" {
		return nil, nil
	}
	spec, err := cliflag.ParseSpec(value)
	if err != nil {
		return nil, err
	}
	return &conversion.PluginSpec{Name: spec.Name, Values: spec.Values}, nil
}

func inferMuxerName(outputs []catalog.OutputFormat, output string) (string, error) {
	extension := strings.ToLower(filepath.Ext(output))
	var match string
	for _, format := range outputs {
		if !slices.Contains(format.Extensions, extension) {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("multiple output formats match %q", extension)
		}
		match = format.Muxer
	}
	if match == "" {
		return "", fmt.Errorf("cannot infer output format from %q; use --format", output)
	}
	return match, nil
}

type pendingOutput struct {
	path, temporary string
	file            *os.File
}

func prepareOutput(command *cobra.Command, path string, force bool) (*pendingOutput, bool, error) {
	if _, err := os.Stat(path); err == nil && !force {
		if !isTerminal(command.InOrStdin()) {
			return nil, false, fmt.Errorf("output %q exists; use --force", path)
		}
		_, _ = fmt.Fprintf(command.ErrOrStderr(), "Overwrite %s? [y/N] ", path)
		answer, _ := bufio.NewReader(command.InOrStdin()).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return nil, true, nil
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".godec-*")
	if err != nil {
		return nil, false, err
	}
	return &pendingOutput{path: path, temporary: temporary.Name(), file: temporary}, false, nil
}

func (output *pendingOutput) abort() {
	if output.file != nil {
		_ = output.file.Close()
	}
	_ = os.Remove(output.temporary)
}
func (output *pendingOutput) commit() error {
	if err := output.file.Close(); err != nil {
		return err
	}
	output.file = nil
	return os.Rename(output.temporary, output.path)
}
func isTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	return ok && terminalFile(file)
}
func terminalFile(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
