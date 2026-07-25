package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	command.Flags().BoolVarP(&options.force, "force", "f", false, "Overwrite an existing output file")
	command.Flags().StringArrayVarP(&options.filters, "filter", "e", nil, "Filter specification (name:key=value,...)")
	command.Flags().StringArrayVarP(&options.inputs, "input", "i", nil, "Named auxiliary input (NAME=PATH)")
	command.Flags().StringArrayVarP(&options.wires, "wire", "w", nil, "Connect FILTER_ALIAS.PORT to SOURCE_ALIAS.PORT (SOURCE may be @in, an --input alias, or another filter's alias); @out.in=SOURCE_ALIAS.PORT sets the pipeline's final output")
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
	inputs   []string
	wires    []string
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
