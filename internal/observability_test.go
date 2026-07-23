package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/pipeline"
)

func writeObservableTestWAV(t *testing.T, path string) {
	t.Helper()
	writeObservableWAV(t, path, 800)
}

func writeObservableWAV(t *testing.T, path string, samples int) {
	t.Helper()
	const sampleRate = 8000
	dataSize := samples * 2
	data := make([]byte, 44)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(36+dataSize))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], sampleRate)
	binary.LittleEndian.PutUint32(data[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(dataSize))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, int64(44+dataSize)); err != nil {
		t.Fatal(err)
	}
}

func executeRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	command := newRootCommand()
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

func TestConvertDryRunDescribesPipelineWithoutCreatingOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	output := filepath.Join(dir, "output.wav")
	writeObservableTestWAV(t, input)
	stdout, stderr, err := executeRoot(t, "convert", input, output, "--dry-run", "--progress=never")
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	for _, expected := range []string{"Input streams:", "duration=100ms", "plugin=wav", "progress-source"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("dry-run output does not contain %q:\n%s", expected, stdout)
		}
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("dry-run created output: %v", err)
	}
}

func TestConvertMetricsReportsSuccessfulRun(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	output := filepath.Join(dir, "output.wav")
	writeObservableTestWAV(t, input)
	_, stderr, err := executeRoot(t, "convert", input, output, "--metrics", "--progress=never")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Starting conversion:\n", "    --> ", "Metrics:", "status: completed", "runtime:", "demuxer:out -> decoder:in", "items=", "Conversion completed successfully."} {
		if !strings.Contains(stderr, expected) {
			t.Fatalf("metrics output does not contain %q:\n%s", expected, stderr)
		}
	}
	if metricsIndex, successIndex := strings.Index(stderr, "Metrics:"), strings.Index(stderr, "Conversion completed successfully."); metricsIndex > successIndex {
		t.Fatalf("success message precedes metrics:\n%s", stderr)
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatalf("output was not committed: info=%v err=%v", info, err)
	}
}

func TestConvertPrintsStartAndSuccessWithoutVerbose(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	output := filepath.Join(dir, "output.wav")
	writeObservableTestWAV(t, input)
	stdout, stderr, err := executeRoot(t, "convert", input, output, "--progress=never")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q", stdout)
	}
	for _, expected := range []string{
		"Starting conversion:\n  input[#0 audio",
		"\n    --> demuxer(wav)\n    --> decoder(pcm)\n    --> encoder(pcm)\n    --> muxer(wav)\n    --> output[#0 audio",
	} {
		if !strings.Contains(stderr, expected) {
			t.Fatalf("conversion output does not contain %q:\n%s", expected, stderr)
		}
	}
	if !strings.HasSuffix(stderr, "Conversion completed successfully.\n") {
		t.Fatalf("success message is not final output:\n%s", stderr)
	}
}

func TestConvertMetricsReportsFailure(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	output := filepath.Join(dir, "output.wav")
	writeObservableTestWAV(t, input)
	_, stderr, err := executeRoot(t, "convert", input, output, "--codec=flac", "--metrics", "--progress=never")
	if err == nil {
		t.Fatal("convert succeeded")
	}
	if !strings.Contains(stderr, "status: failed") || !strings.Contains(stderr, "muxer \"wav\" does not support codec \"flac\"") {
		t.Fatalf("failure metrics = %s", stderr)
	}
	if strings.Contains(stderr, "Conversion completed successfully.") {
		t.Fatalf("failure output contains success message: %s", stderr)
	}
}

func TestConvertMetricsReportsCancellation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.wav")
	output := filepath.Join(dir, "output.wav")
	writeObservableWAV(t, input, 16<<20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := newRootCommand()
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetContext(ctx)
	command.SetArgs([]string{"convert", input, output, "--metrics", "--progress=never"})
	err := command.Execute()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !strings.Contains(stderr.String(), "status: canceled") || !strings.Contains(stderr.String(), "runtime:") {
		t.Fatalf("cancellation metrics = %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "Conversion completed successfully.") {
		t.Fatalf("cancellation output contains success message: %s", stderr.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("canceled conversion committed output: %v", err)
	}
}

func TestDryRunRejectsMetrics(t *testing.T) {
	_, _, err := executeRoot(t, "convert", "input.wav", "output.wav", "--dry-run", "--metrics")
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveProgressConfig(t *testing.T) {
	if config, err := resolveProgressConfig("auto", false, false); err != nil || config.enabled {
		t.Fatalf("non-terminal auto = %#v, %v", config, err)
	}
	if config, err := resolveProgressConfig("auto", true, false); err != nil || !config.enabled || !config.terminal {
		t.Fatalf("terminal auto = %#v, %v", config, err)
	}
	if _, err := resolveProgressConfig("sometimes", true, false); err == nil {
		t.Fatal("invalid progress mode accepted")
	}
	if config, err := resolveProgressConfig("always", false, false); err != nil || !config.enabled || config.terminal || config.interval != time.Second {
		t.Fatalf("non-terminal always = %#v, %v", config, err)
	}
	if config, err := resolveProgressConfig("always", true, false); err != nil || config.interval != 250*time.Millisecond {
		t.Fatalf("terminal always = %#v, %v", config, err)
	}
	if config, err := resolveProgressConfig("always", true, true); err != nil || config.enabled {
		t.Fatalf("dry-run progress = %#v, %v", config, err)
	}
}

func TestFormatProgressPrefersMediaTimeAndCompletes(t *testing.T) {
	snapshot := pipeline.Snapshot{
		Elapsed: 2 * time.Second,
		Edges: []pipeline.EdgeSnapshot{{
			Description: pipeline.EdgeDescription{ProgressSource: true, Stream: media.StreamInfo{Duration: 10 * time.Second}},
			MediaTime:   4 * time.Second,
		}},
	}
	if line := formatProgress(snapshot, inputMetrics{Position: 9, Size: 10}, false); !strings.Contains(line, "40.00%") || !strings.Contains(line, "2.00x") {
		t.Fatalf("progress = %q", line)
	}
	if line := formatProgress(snapshot, inputMetrics{}, true); !strings.Contains(line, "100.00%") {
		t.Fatalf("completed progress = %q", line)
	}
}
