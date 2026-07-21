package cli

import (
	"context"
	"fmt"
	"io"

	godec "github.com/godexture/core"
	"github.com/godexture/core/registry"
	"github.com/spf13/cobra"
)

func Execute(ctx context.Context, args []string) error {
	root := newRootCommand()
	root.SetArgs(args)
	root.SetContext(ctx)
	return root.Execute()
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "godec", SilenceUsage: true}
	root.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose logging")
	root.AddCommand(newListCommand())
	root.AddCommand(newDescribeCommand())
	root.AddCommand(newConvertCommand())
	return root
}

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list [muxers|demuxers|decoders|encoders|filters]",
		Short: "List available plugins",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				for _, role := range []string{"muxers", "demuxers", "encoders", "decoders", "filters"} {
					if err := writeListedRole(command.OutOrStdout(), role); err != nil {
						return err
					}
				}
				return nil
			}
			return writeListedRole(command.OutOrStdout(), args[0])
		},
	}
}

func writeListedRole(writer io.Writer, role string) error {
	switch role {
	case "muxers":
		return writeRole(writer, role, godec.DefaultMuxerRegistry)
	case "demuxers":
		return writeRole(writer, role, godec.DefaultDemuxerRegistry)
	case "encoders":
		return writeRole(writer, role, godec.DefaultEncoderRegistry)
	case "decoders":
		return writeRole(writer, role, godec.DefaultDecoderRegistry)
	case "filters":
		return writeRole(writer, role, godec.DefaultFilterRegistry)
	default:
		return fmt.Errorf("unknown plugin role %q; use muxers, demuxers, encoders, decoders, or filters", role)
	}
}

func writeRole[V registry.Manifest](writer io.Writer, title string, values *registry.Registry[V]) error {
	names := values.Names()
	_, _ = fmt.Fprintf(writer, "%s:\n", title)
	for _, name := range names {
		manifest, err := values.Lookup(name)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(writer, "  %-12s %s\n", name, manifestDescription(manifest))
	}
	return nil
}

func manifestDescription(manifest registry.Manifest) string {
	switch manifest := any(manifest).(type) {
	case registry.MuxerManifest:
		return manifest.Description
	case registry.DemuxerManifest:
		return manifest.Description
	case registry.EncoderManifest:
		return manifest.Description
	case registry.DecoderManifest:
		return manifest.Description
	case registry.FilterManifest:
		return manifest.Description
	default:
		return ""
	}
}
