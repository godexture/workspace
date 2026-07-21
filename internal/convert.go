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
	"github.com/godexture/sdk/cliflag"
	"github.com/spf13/cobra"
)

func newConvertCommand() *cobra.Command {
	var format, codec string
	var jobs int
	var force bool
	var filters []string
	var configs map[string]registry.Configuration
	command := &cobra.Command{
		Use:  "convert INPUT OUTPUT",
		Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return runConvert(command, args[0], args[1], format, codec, jobs, force, filters, configs)
		},
	}
	command.Flags().StringVar(&format, "format", "", "Output format")
	command.Flags().StringVar(&codec, "codec", "", "Output codec specification (name:key=value,...)")
	command.Flags().IntVarP(&jobs, "jobs", "j", 0, "Maximum parallel jobs")
	command.Flags().BoolVar(&force, "force", false, "Overwrite an existing output file")
	command.Flags().StringArrayVar(&filters, "filter", nil, "Filter specification (name:key=value,...)")
	bindings, configs, err := bindMuxerConfigurations(command)
	if err != nil {
		panic(err)
	}
	command.PreRunE = func(_ *cobra.Command, _ []string) error {
		for _, binding := range bindings {
			if err := binding.binding.Apply(binding.config); err != nil {
				return err
			}
		}
		return nil
	}
	return command
}

type configBinding struct {
	config  registry.Configuration
	binding *cliflag.Binding
}

func bindMuxerConfigurations(command *cobra.Command) ([]configBinding, map[string]registry.Configuration, error) {
	bindings := make([]configBinding, 0)
	configs := make(map[string]registry.Configuration)
	bind := func(namespace string, config registry.Configuration) error {
		binding, err := cliflag.BindStruct(command.Flags(), namespace, config)
		if err != nil {
			return err
		}
		bindings = append(bindings, configBinding{config: config, binding: binding})
		configs[namespace] = config
		return nil
	}
	for manifest := range godec.DefaultMuxerRegistry.Enumerate() {
		config, err := manifest.NewConfiguration()
		if err != nil {
			return nil, nil, err
		}
		if err := bind("muxer."+manifest.Name, config); err != nil {
			return nil, nil, err
		}
	}
	return bindings, configs, nil
}

func runConvert(command *cobra.Command, inputPath, outputPath, format, codec string, jobs int, force bool, filters []string, configs map[string]registry.Configuration) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer input.Close()
	muxer, err := selectMuxer(format, outputPath)
	if err != nil {
		return err
	}
	muxConfig := configs["muxer."+muxer.Name]
	targetCodec, codecValues, err := resolveCodec(codec, muxer.DefaultCodec)
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
	encoderConfig, err := encoder.NewConfiguration()
	if err != nil {
		return err
	}
	if err := cliflag.DecodeStruct(encoderConfig, codecValues); err != nil {
		return fmt.Errorf("codec %q: %w", encoder.Name, err)
	}
	filterSpecs, err := resolveFilters(filters)
	if err != nil {
		return err
	}
	output, skip, err := prepareOutput(command, outputPath, force)
	if err != nil || skip {
		return err
	}
	defer output.abort()
	geometry, err := godec.NewNegotiator().NegotiateConversion(command.Context(), routing.ConversionSpec{
		Input: input, Output: output.file, Filters: filterSpecs, TargetCodec: targetCodec, EncodeConfig: encoderConfig, MuxConfig: muxConfig,
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

func resolveCodec(value string, defaultCodec media.CodecID) (media.CodecID, map[string]string, error) {
	if value == "" {
		return defaultCodec, nil, nil
	}
	spec, err := cliflag.ParseSpec(value)
	if err != nil {
		return "", nil, fmt.Errorf("codec: %w", err)
	}
	return media.CodecID(spec.Name), spec.Values, nil
}

func resolveFilters(values []string) ([]routing.FilterSpec, error) {
	filters := make([]routing.FilterSpec, 0, len(values))
	for _, value := range values {
		spec, err := cliflag.ParseSpec(value)
		if err != nil {
			return nil, err
		}
		manifest, err := godec.DefaultFilterRegistry.Lookup(spec.Name)
		if err != nil {
			return nil, err
		}
		config, err := manifest.NewConfiguration()
		if err != nil {
			return nil, err
		}
		if err := cliflag.DecodeStruct(config, spec.Values); err != nil {
			return nil, fmt.Errorf("filter %q: %w", spec.Name, err)
		}
		filters = append(filters, routing.FilterSpec{Config: config})
	}
	return filters, nil
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
