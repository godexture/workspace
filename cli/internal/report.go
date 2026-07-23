package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/cliflag"
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
	if _, err := fmt.Fprintln(writer, "Pipeline:"); err != nil {
		return err
	}
	for _, node := range description.Nodes {
		if _, err := fmt.Fprintf(writer, "  %s  role=%s plugin=%s", node.ID, node.Role, node.Plugin); err != nil {
			return err
		}
		if node.Resources.Parallelism > 0 {
			if _, err := fmt.Fprintf(writer, " parallelism=%d", node.Resources.Parallelism); err != nil {
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
	parts := make([]string, 0, len(description.Nodes)+1)
	for _, edge := range description.Edges {
		if edge.ProgressSource {
			if _, err := fmt.Fprintf(writer, "  input[%s]\n", formatStream(edge.Stream)); err != nil {
				return err
			}
			break
		}
	}
	for _, current := range description.Nodes {
		label := string(current.Role)
		if label == "" {
			label = current.ID
		}
		if current.Plugin != "" {
			label += "(" + current.Plugin + ")"
		}
		parts = append(parts, label)
	}
	for _, current := range description.Nodes {
		if current.Role == manifest.RoleMuxer && len(current.Inputs) > 0 {
			parts = append(parts, "output["+formatStream(current.Inputs[0])+"]")
			break
		}
	}
	for _, part := range parts {
		if _, err := fmt.Fprintf(writer, "    --> %s\n", part); err != nil {
			return err
		}
	}
	return nil
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

type phaseMetrics struct {
	Negotiation time.Duration // includes pipeline build; conversion.Build() performs both atomically
	Execution   time.Duration
	Finalize    time.Duration
	Total       time.Duration
}

type metricsReport struct {
	Err      error
	Phases   phaseMetrics
	Input    inputMetrics
	Output   outputMetrics
	Runtime  runtimeMetrics
	Pipeline pipeline.Snapshot
}

func writeMetricsReport(writer io.Writer, report metricsReport) error {
	status := "completed"
	if report.Err != nil {
		status = "failed"
		if errors.Is(report.Err, context.Canceled) {
			status = "canceled"
		}
	}
	if _, err := fmt.Fprintf(writer, "Metrics:\n  status: %s\n", status); err != nil {
		return err
	}
	if report.Err != nil {
		if _, err := fmt.Fprintf(writer, "  error: %s\n", report.Err); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer,
		"  timing: negotiation=%s execution=%s finalize=%s total=%s\n",
		formatMetricDuration(report.Phases.Negotiation),
		formatMetricDuration(report.Phases.Execution), formatMetricDuration(report.Phases.Finalize), formatMetricDuration(report.Phases.Total)); err != nil {
		return err
	}
	executionSeconds := report.Phases.Execution.Seconds()
	inputRate, outputRate := float64(0), float64(0)
	var maximumMediaTime time.Duration
	for _, edge := range report.Pipeline.Edges {
		maximumMediaTime = max(maximumMediaTime, edge.MediaTime)
	}
	realTimeRatio := 0.0
	if executionSeconds > 0 {
		inputRate = float64(report.Input.BytesRead) / executionSeconds
		outputRate = float64(report.Output.BytesWritten) / executionSeconds
		realTimeRatio = maximumMediaTime.Seconds() / executionSeconds
	}
	if _, err := fmt.Fprintf(writer, "  execution: real-time-ratio=%.2fx\n", realTimeRatio); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer,
		"  input: read=%s calls=%d seeks=%d rate=%s/s position=%d/%d\n",
		formatBytes(report.Input.BytesRead), report.Input.ReadCalls, report.Input.SeekCalls, formatBytes(uint64(inputRate)), report.Input.Position, report.Input.Size); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "  output: wrote=%s calls=%d rate=%s/s\n",
		formatBytes(report.Output.BytesWritten), report.Output.WriteCalls, formatBytes(uint64(outputRate))); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer,
		"  runtime: heap=%s peak-heap=%s heap-inuse=%s peak-inuse=%s sys=%s peak-sys=%s total-alloc=%s mallocs=%d frees=%d gc=%d gc-pause=%s peak-goroutines=%d\n",
		formatBytes(report.Runtime.HeapAlloc), formatBytes(report.Runtime.PeakHeapAlloc),
		formatBytes(report.Runtime.HeapInuse), formatBytes(report.Runtime.PeakHeapInuse),
		formatBytes(report.Runtime.System), formatBytes(report.Runtime.PeakSystem),
		formatBytes(report.Runtime.TotalAllocated), report.Runtime.Mallocs, report.Runtime.Frees,
		report.Runtime.GCs, formatMetricDuration(report.Runtime.GCPause), report.Runtime.PeakGoroutines); err != nil {
		return err
	}
	if len(report.Pipeline.Nodes) > 0 {
		if _, err := fmt.Fprintln(writer, "  nodes:"); err != nil {
			return err
		}
		for _, node := range report.Pipeline.Nodes {
			if _, err := fmt.Fprintf(writer, "    %s: state=%s elapsed=%s parallelism=%d",
				node.Description.ID, node.State, formatMetricDuration(node.Elapsed), node.Description.Resources.Parallelism); err != nil {
				return err
			}
			if node.Error != "" {
				if _, err := fmt.Fprintf(writer, " error=%q", node.Error); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
	}
	if len(report.Pipeline.Edges) > 0 {
		if _, err := fmt.Fprintln(writer, "  edges:"); err != nil {
			return err
		}
		for _, edge := range report.Pipeline.Edges {
			itemsRate, bytesRate, mediaRate := float64(0), float64(0), float64(0)
			if executionSeconds > 0 {
				itemsRate = float64(edge.Items) / executionSeconds
				bytesRate = float64(edge.Bytes) / executionSeconds
				mediaRate = edge.MediaTime.Seconds() / executionSeconds
			}
			if _, err := fmt.Fprintf(writer, "    %s:%s -> %s:%s: items=%d bytes=%s samples=%d media-time=%s item-rate=%.2f/s payload-rate=%s/s media-rate=%.2fx\n",
				edge.Description.FromNode, edge.Description.FromPort, edge.Description.ToNode, edge.Description.ToPort,
				edge.Items, formatBytes(edge.Bytes), edge.Samples, formatMetricDuration(edge.MediaTime),
				itemsRate, formatBytes(uint64(bytesRate)), mediaRate); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor, exponent := uint64(unit), 0
	for value := bytes / unit; value >= unit && exponent < 5; value /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.2f %ciB", float64(bytes)/float64(divisor), "KMGTPE"[exponent])
}

func formatElapsed(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}

	t := time.Unix(0, 0).UTC().Add(duration)
	return t.Format("15:04:05.0000")
}

func formatMetricDuration(duration time.Duration) string {
	return duration.Round(time.Millisecond).String()
}
