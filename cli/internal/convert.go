package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/routing"
	"github.com/spf13/cobra"
)

func newConvertCommand() *cobra.Command {
	var format, codec string
	var jobs int
	var force bool
	command := &cobra.Command{
		Use:  "convert INPUT OUTPUT",
		Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runConvert(command, args[0], args[1], format, codec, jobs, force)
		},
	}
	command.Flags().StringVar(&format, "format", "", "Output format")
	command.Flags().StringVar(&codec, "codec", "", "Output codec")
	command.Flags().IntVarP(&jobs, "jobs", "j", 0, "Maximum parallel jobs")
	command.Flags().BoolVar(&force, "force", false, "Overwrite an existing output file")
	return command
}

func runConvert(command *cobra.Command, inputPath, outputPath, format, codec string, jobs int, force bool) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer input.Close()
	muxer, err := selectMuxer(format, outputPath)
	if err != nil {
		return err
	}
	muxConfig, err := muxer.NewConfiguration()
	if err != nil {
		return err
	}
	targetCodec := muxer.DefaultCodec
	if codec != "" {
		targetCodec = media.CodecID(codec)
	}
	if !muxer.Supports(targetCodec) {
		return fmt.Errorf("format %q does not support codec %q", muxer.Name, targetCodec)
	}
	output, skip, err := prepareOutput(command, outputPath, force)
	if err != nil || skip {
		return err
	}
	defer output.abort()
	geometry, err := godec.NewNegotiator().NegotiateConversion(command.Context(), routing.ConversionSpec{
		Input: input, Output: output.file, TargetCodec: targetCodec, MuxConfig: muxConfig,
		Resources: registry.ResourceBudget{Parallelism: jobs},
	})
	if err != nil {
		return err
	}
	conversion, err := godec.NewBuilder().Build(geometry)
	if err != nil {
		_ = geometry.Close()
		return err
	}
	defer conversion.Close()
	if err := conversion.Run(command.Context()); err != nil {
		return err
	}
	return output.commit()
}

func selectMuxer(name, output string) (registry.MuxerManifest, error) {
	if name != "" {
		return godec.DefaultMuxerRegistry.Lookup(name)
	}
	extension := strings.ToLower(filepath.Ext(output))
	var match registry.MuxerManifest
	for manifest := range godec.DefaultMuxerRegistry.Enumerate() {
		if strings.EqualFold(filepath.Ext(output), extension) && slicesContain(manifest.Extensions, extension) {
			if match.Name != "" {
				return match, fmt.Errorf("multiple output formats match %q", extension)
			}
			match = manifest
		}
	}
	if match.Name == "" {
		return match, fmt.Errorf("cannot infer output format from %q; use --format", output)
	}
	return match, nil
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
