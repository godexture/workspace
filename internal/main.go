package cli

import (
	"context"
	"fmt"
	"io"
	"sort"

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
	root.AddCommand(newListCommand())
	root.AddCommand(newConvertCommand())
	return root
}

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use: "list",
		RunE: func(command *cobra.Command, _ []string) error {
			writeRole(command.OutOrStdout(), "muxers", godec.DefaultMuxerRegistry)
			writeRole(command.OutOrStdout(), "demuxers", godec.DefaultDemuxerRegistry)
			writeRole(command.OutOrStdout(), "encoders", godec.DefaultEncoderRegistry)
			writeRole(command.OutOrStdout(), "decoders", godec.DefaultDecoderRegistry)
			writeRole(command.OutOrStdout(), "filters", godec.DefaultFilterRegistry)
			return nil
		},
	}
}

func writeRole[V registry.Manifest](writer io.Writer, title string, values *registry.Registry[V]) {
	names := values.Names()
	sort.Strings(names)
	_, _ = fmt.Fprintf(writer, "%s:\n", title)
	for _, name := range names {
		_, _ = fmt.Fprintf(writer, "  %s\n", name)
	}
}
