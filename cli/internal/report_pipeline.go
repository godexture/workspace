package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/sdk/cliflag"
)

func writePipelineDescription(writer io.Writer, description pipeline.Description) error {
	selected := selectedInput(description)
	if _, err := fmt.Fprintln(writer, "Input streams:"); err != nil {
		return err
	}
	for _, node := range description.Nodes {
		if node.Role != manifest.RoleDemuxer {
			continue
		}
		for _, stream := range node.Outputs {
			marker := " "
			if stream.Index == selected {
				marker = "*"
			}
			if _, err := fmt.Fprintf(writer, "  %s %s\n", marker, formatStream(stream)); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(writer, "Output streams:"); err != nil {
		return err
	}
	for _, node := range description.Nodes {
		if node.Role != manifest.RoleMuxer {
			continue
		}
		for _, stream := range node.Inputs {
			if _, err := fmt.Fprintf(writer, "    %s\n", formatStream(stream)); err != nil {
				return err
			}
		}
	}
	if err := writePipelineChains(writer, description); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "Pipeline:"); err != nil {
		return err
	}
	for _, node := range description.Nodes {
		if _, err := fmt.Fprintf(writer, "  %s  role=%s plugin=%s", node.ID, node.Role, node.Plugin); err != nil {
			return err
		}
		if parallelism := node.Resources.Parallelism(); parallelism > 0 {
			if _, err := fmt.Fprintf(writer, " parallelism=%d", parallelism); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
		if err := writeConfiguration(writer, node.Configuration); err != nil {
			return err
		}
		for _, stream := range node.Inputs {
			if _, err := fmt.Fprintf(writer, "    in:  %s\n", formatStream(stream)); err != nil {
				return err
			}
		}
		for _, stream := range node.Outputs {
			if _, err := fmt.Fprintf(writer, "    out: %s\n", formatStream(stream)); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(writer, "Edges:"); err != nil {
		return err
	}
	for _, edge := range description.Edges {
		marker := ""
		if edge.ProgressSource {
			marker = " progress-source"
		}
		if _, err := fmt.Fprintf(writer, "  %s:%s -> %s:%s%s  %s\n",
			edge.FromNode, edge.FromPort, edge.ToNode, edge.ToPort, marker, formatStream(edge.Stream)); err != nil {
			return err
		}
	}
	return nil
}

func writeConversionStart(writer io.Writer, description pipeline.Description) error {
	if _, err := fmt.Fprintln(writer, "Starting conversion:"); err != nil {
		return err
	}
	return writePipelineChains(writer, description)
}

func writePipelineChains(writer io.Writer, description pipeline.Description) error {
	nodes := make(map[string]pipeline.NodeDescription, len(description.Nodes))
	starts := make([]string, 0)
	for _, node := range description.Nodes {
		nodes[node.ID] = node
		if node.Role == manifest.RoleDemuxer {
			starts = append(starts, node.ID)
		}
	}
	slices.SortFunc(starts, func(left, right string) int {
		if left == "demuxer" {
			return -1
		}
		if right == "demuxer" {
			return 1
		}
		return strings.Compare(left, right)
	})
	if _, err := fmt.Fprintln(writer, "Streams:"); err != nil {
		return err
	}
	for _, start := range starts {
		parts := []string{formatPipelineNode(nodes[start])}
		current := start
		seen := map[string]bool{start: true}
		for {
			edge, found := pipelineNextEdge(description.Edges, current)
			if !found {
				break
			}
			if edge.ToPort != "in" {
				parts = append(parts, edge.ToNode+"."+edge.ToPort)
				break
			}
			if seen[edge.ToNode] {
				parts = append(parts, edge.ToNode+" (cycle)")
				break
			}
			next, ok := nodes[edge.ToNode]
			if !ok {
				parts = append(parts, edge.ToNode)
				break
			}
			parts = append(parts, formatPipelineNode(next))
			seen[edge.ToNode] = true
			current = edge.ToNode
		}
		name := "main"
		if start != "demuxer" {
			name = strings.TrimSuffix(strings.TrimPrefix(start, "aux:"), ":demuxer")
		}
		if _, err := fmt.Fprintf(writer, "  %s: %s\n", name, strings.Join(parts, " -> ")); err != nil {
			return err
		}
	}
	return nil
}

func pipelineNextEdge(edges []pipeline.EdgeDescription, nodeID string) (pipeline.EdgeDescription, bool) {
	for _, edge := range edges {
		if edge.FromNode == nodeID && edge.FromPort == "out" && edge.ToPort == "in" {
			return edge, true
		}
	}
	for _, edge := range edges {
		if edge.FromNode == nodeID && edge.FromPort == "out" {
			return edge, true
		}
	}
	return pipeline.EdgeDescription{}, false
}

func formatPipelineNode(node pipeline.NodeDescription) string {
	if node.Plugin == "" {
		return node.ID
	}
	return node.ID + "(" + node.Plugin + ")"
}

func writeConfiguration(writer io.Writer, configuration registry.Configuration) error {
	if configuration == nil {
		return nil
	}
	values, err := cliflag.StructValues(configuration)
	if err != nil {
		return fmt.Errorf("describe configuration: %w", err)
	}
	if len(values) == 0 {
		return nil
	}
	items := make([]string, len(values))
	for i, field := range values {
		items[i] = field.Name + "=" + field.Value
	}
	_, err = fmt.Fprintf(writer, "    config: %s\n", strings.Join(items, ", "))
	return err
}

func selectedInput(description pipeline.Description) int {
	for _, edge := range description.Edges {
		if edge.ProgressSource {
			return edge.Stream.Index
		}
	}
	return -1
}

func formatStream(stream media.StreamInfo) string {
	parts := []string{fmt.Sprintf("#%d", stream.Index), string(stream.Type)}
	if stream.Codec != "" {
		parts = append(parts, "codec="+string(stream.Codec))
	}
	if stream.Duration > 0 {
		parts = append(parts, "duration="+stream.Duration.Round(time.Millisecond).String())
	}
	if stream.Type == media.MediaAudio {
		audio := stream.Audio
		if audio.SampleRate > 0 {
			parts = append(parts, fmt.Sprintf("rate=%dHz", audio.SampleRate))
		}
		if audio.Format != "" {
			format := "format=" + string(audio.Format)
			if bits := audio.EffectiveBitsPerSample(); bits > 0 {
				format += fmt.Sprintf("/%dbit", bits)
			}
			parts = append(parts, format)
		}
		if channels := audio.ChannelCount(); channels > 0 {
			layout := audio.ChannelLayout.String()
			parts = append(parts, fmt.Sprintf("channels=%d/%s", channels, layout))
		}
	}
	if stream.IsDefault {
		parts = append(parts, "default")
	}
	return strings.Join(parts, " ")
}
