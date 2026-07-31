package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/godexture/godec/sdk/catalog"
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
	root.AddCommand(newPlayCommand())
	return root
}

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list [formats|codecs|muxers|demuxers|decoders|encoders|filters]",
		Short: "List available plugins",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			value := catalog.Build()
			if len(args) == 0 {
				for _, role := range []string{"muxers", "demuxers", "encoders", "decoders", "filters"} {
					if err := writeListedRole(command.OutOrStdout(), value, role); err != nil {
						return err
					}
				}
				return nil
			}
			return writeListedRole(command.OutOrStdout(), value, args[0])
		},
	}
}

func writeListedRole(writer io.Writer, value catalog.Catalog, role string) error {
	switch role {
	case "formats":
		writeEntries(writer, "muxers", value.Muxers)
		writeEntries(writer, "demuxers", value.Demuxers)
	case "codecs":
		writeEntries(writer, "encoders", value.Encoders)
		writeEntries(writer, "decoders", value.Decoders)
	case "muxers":
		writeEntries(writer, role, value.Muxers)
	case "demuxers":
		writeEntries(writer, role, value.Demuxers)
	case "encoders":
		writeEntries(writer, role, value.Encoders)
	case "decoders":
		writeEntries(writer, role, value.Decoders)
	case "filters":
		writeEntries(writer, role, filterPlugins(value.Filters))
	default:
		return fmt.Errorf("unknown plugin role %q; use formats, codecs, muxers, demuxers, encoders, decoders, or filters", role)
	}
	return nil
}

func filterPlugins(entries []catalog.FilterEntry) []catalog.PluginEntry {
	plugins := make([]catalog.PluginEntry, len(entries))
	for i, entry := range entries {
		plugins[i] = entry.PluginEntry
	}
	return plugins
}

func writeEntries(writer io.Writer, title string, entries []catalog.PluginEntry) {
	_, _ = fmt.Fprintf(writer, "%s:\n", title)
	for _, entry := range entries {
		_, _ = fmt.Fprintf(writer, "  %-12s %s\n", entry.Name, entry.Description)
	}
}
