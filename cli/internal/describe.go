package cli

import (
	"fmt"
	"io"
	"strings"

	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/sdk/cliflag"
	"github.com/spf13/cobra"
)

func newDescribeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "describe {format|codec|demuxer|decoder|filter} NAME",
		Short: "Describe plugin configuration",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			selector, manifest, err := describeManifest(args[0], args[1])
			if err != nil {
				return err
			}
			config, err := manifest.NewConfiguration()
			if err != nil {
				return err
			}
			return writeDescription(command.OutOrStdout(), selector, args[1], config)
		},
	}
}

func describeManifest(kind, name string) (string, registry.Manifest, error) {
	switch kind {
	case "format":
		manifest, err := godec.DefaultMuxerRegistry.Lookup(name)
		return "format", manifest, err
	case "codec":
		manifest, err := godec.NewResolver().NewEncoderResolver(godec.DefaultEncoderRegistry).ResolveEncoder(media.CodecID(name))
		return "codec", manifest, err
	case "demuxer":
		manifest, err := godec.DefaultDemuxerRegistry.Lookup(name)
		return "demuxer", manifest, err
	case "decoder":
		manifest, err := godec.DefaultDecoderRegistry.Lookup(name)
		return "decoder", manifest, err
	case "filter":
		manifest, err := godec.DefaultFilterRegistry.Lookup(name)
		return "filter", manifest, err
	default:
		return "", nil, fmt.Errorf("unknown plugin kind %q; use format, codec, demuxer, decoder, or filter", kind)
	}
}

func writeDescription(writer io.Writer, selector, name string, config registry.Configuration) error {
	fields, err := cliflag.DescribeStruct(config)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(writer, "Usage:\n  --%s %s", selector, name)
	if len(fields) > 0 {
		_, _ = fmt.Fprint(writer, "[:key=value,...]")
	}
	_, _ = fmt.Fprintln(writer)
	if len(fields) == 0 {
		return nil
	}
	_, _ = fmt.Fprintln(writer, "\nOptions:")
	for _, field := range fields {
		_, _ = fmt.Fprintf(writer, "  %-24s %-10s %s [default: %s]\n", field.Name, field.Type, field.Help, field.Default)
		if len(field.Choices) > 0 {
			_, _ = fmt.Fprintf(writer, "    choices: %s\n", strings.Join(field.Choices, ", "))
		}
	}
	return nil
}
