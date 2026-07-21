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

func resolvePlugin[V registry.Manifest](role, value string, plugins *registry.Registry[V]) (V, registry.Configuration, error) {
	var zero V
	if value == "" {
		return zero, nil, nil
	}
	spec, err := cliflag.ParseSpec(value)
	if err != nil {
		return zero, nil, fmt.Errorf("%s: %w", role, err)
	}
	manifest, err := plugins.Lookup(spec.Name)
	if err != nil {
		return zero, nil, err
	}
	config, err := configureManifest(role, manifest, spec.Values)
	if err != nil {
		return zero, nil, err
	}
	return manifest, config, nil
}

func configureManifest(role string, manifest registry.Manifest, values map[string]string) (registry.Configuration, error) {
	config, err := manifest.NewConfiguration()
	if err != nil {
		return nil, err
	}
	if err := cliflag.DecodeStruct(config, values); err != nil {
		return nil, fmt.Errorf("%s %q: %w", role, manifest.RegistryName(), err)
	}
	return config, nil
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
		config, err := configureManifest("filter", manifest, spec.Values)
		if err != nil {
			return nil, err
		}
		filters = append(filters, routing.FilterSpec{Config: config})
	}
	return filters, nil
}

func selectMuxer(value, output string) (registry.MuxerManifest, map[string]string, error) {
	if value != "" {
		spec, err := cliflag.ParseSpec(value)
		if err != nil {
			return registry.MuxerManifest{}, nil, fmt.Errorf("format: %w", err)
		}
		manifest, err := godec.DefaultMuxerRegistry.Lookup(spec.Name)
		return manifest, spec.Values, err
	}
	extension := strings.ToLower(filepath.Ext(output))
	var match registry.MuxerManifest
	for manifest := range godec.DefaultMuxerRegistry.Enumerate() {
		if strings.EqualFold(filepath.Ext(output), extension) && slicesContain(manifest.Extensions, extension) {
			if match.Name != "" {
				return match, nil, fmt.Errorf("multiple output formats match %q", extension)
			}
			match = manifest
		}
	}
	if match.Name == "" {
		return match, nil, fmt.Errorf("cannot infer output format from %q; use --format", output)
	}
	return match, nil, nil
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
