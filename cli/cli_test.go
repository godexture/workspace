package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/standard"
)

func TestRunConvertsWaveToRequestedFileFormat(t *testing.T) {
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	input := pcmWave(2, 44_100, 16, payload)
	for _, extension := range []string{"raw", "wav"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			inputPath := filepath.Join(directory, "input.wav")
			outputPath := filepath.Join(directory, "output."+extension)
			if err := os.WriteFile(inputPath, input, 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := Run(t.Context(), instance, []string{inputPath, outputPath}, WithStreams(&stdout, &stderr))
			if code != ExitSuccess {
				t.Fatalf("exit = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			encoded, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if extension == "raw" && !bytes.Equal(encoded, payload) {
				t.Fatalf("raw output = %x, want %x", encoded, payload)
			}
			if extension == "wav" && (!bytes.HasPrefix(encoded, []byte("RIFF")) || !bytes.Contains(encoded, payload)) {
				t.Fatalf("WAVE output = %x", encoded)
			}
			if !strings.Contains(stdout.String(), "origin=automatic reason=format.output") || !strings.Contains(stdout.String(), "state=committed") {
				t.Fatalf("Plan/result output = %s", stdout.String())
			}
			if !strings.Contains(stderr.String(), "progress sequence=") {
				t.Fatalf("progress output = %s", stderr.String())
			}
			if strings.Contains(stderr.String(), "observation-loss") {
				t.Fatalf("ordinary conversion reported observation loss: %s", stderr.String())
			}
			sequences := renderedSequences(t, stderr.String())
			for index, sequence := range sequences {
				if sequence != uint64(index) {
					t.Fatalf("event sequences = %v", sequences)
				}
			}
		})
	}
}

func TestRunPreviewDoesNotCreateOrReplaceOutput(t *testing.T) {
	instance, _ := standard.NewHost()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.wav")
	if err := os.WriteFile(inputPath, pcmWave(1, 48_000, 16, []byte{1, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	original := []byte("existing output")
	if err := os.WriteFile(outputPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), instance, []string{"--plan", inputPath, outputPath}, WithStreams(&stdout, &stderr))
	if code != ExitSuccess || !strings.HasPrefix(stdout.String(), "plan ") || stderr.Len() != 0 {
		t.Fatalf("preview = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	got, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("preview changed target = %q, %v", got, err)
	}
}

func TestRunAcceptsExplicitRawPropertiesWithoutCLIDefaults(t *testing.T) {
	instance, _ := standard.NewHost()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input")
	outputPath := filepath.Join(directory, "output.wav")
	payload := []byte{1, 0, 2, 0}
	if err := os.WriteFile(inputPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), instance, []string{
		"--input-format", "raw",
		"--raw-rate", "48000",
		"--raw-valid-bits", "16",
		"--raw-layout", "mono",
		"--raw-endian", "little",
		inputPath, outputPath,
	}, WithStreams(&stdout, &stderr))
	if code != ExitSuccess {
		t.Fatalf("raw exit = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil || !bytes.HasPrefix(encoded, []byte("RIFF")) || !bytes.Contains(encoded, payload) {
		t.Fatalf("raw WAVE output = %x, %v", encoded, err)
	}
	if !strings.Contains(stdout.String(), "without content evidence") || !strings.Contains(stdout.String(), "config rate=48000 source=planner") {
		t.Fatalf("raw Plan omitted provenance/warning: %s", stdout.String())
	}
}

func TestRunReportsStableUsagePlanningRuntimeAndCancellationCodes(t *testing.T) {
	instance, _ := standard.NewHost()
	directory := t.TempDir()
	wavePath := filepath.Join(directory, "input.wav")
	rawPath := filepath.Join(directory, "input.raw")
	if err := os.WriteFile(wavePath, pcmWave(1, 48_000, 16, []byte{1, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, []byte{1, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	existingOutput := filepath.Join(directory, "existing.wav")
	if err := os.WriteFile(existingOutput, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		ctx  context.Context
		args []string
		want ExitCode
		text string
	}{
		{name: "usage", ctx: t.Context(), args: []string{wavePath}, want: ExitUsage, text: "usage error"},
		{name: "planning", ctx: t.Context(), args: []string{rawPath, filepath.Join(directory, "planning.wav")}, want: ExitPlanning, text: "prepare.format-config-required"},
		{name: "input inspection", ctx: t.Context(), args: []string{filepath.Join(directory, "missing.wav"), existingOutput}, want: ExitPlanning, text: "missing.wav"},
		{name: "same file", ctx: t.Context(), args: []string{wavePath, wavePath}, want: ExitPlanning, text: "prepare.boundary-conflict"},
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	tests = append(tests, struct {
		name string
		ctx  context.Context
		args []string
		want ExitCode
		text string
	}{name: "canceled", ctx: canceled, args: []string{wavePath, filepath.Join(directory, "canceled.wav")}, want: ExitCanceled, text: "canceled"})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(test.ctx, instance, test.args, WithStreams(&stdout, &stderr))
			if code != test.want || !strings.Contains(strings.ToLower(stderr.String()), strings.ToLower(test.text)) {
				t.Fatalf("exit = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
		})
	}

	t.Run("renderer", func(t *testing.T) {
		outputPath := filepath.Join(directory, "renderer.wav")
		code := Run(t.Context(), instance, []string{wavePath, outputPath}, WithStreams(io.Discard, failingWriter{}))
		if code != ExitRuntime {
			t.Fatalf("renderer exit = %d", code)
		}
	})
}

func TestRunDoesNotBackpressureConversionOnBlockedRenderer(t *testing.T) {
	instance, _ := standard.NewHost()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.wav")
	payload := bytes.Repeat([]byte{1, 0, 2, 0}, 4_096)
	if err := os.WriteFile(inputPath, pcmWave(2, 44_100, 16, payload), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := newBlockingWriter()
	var stdout bytes.Buffer
	finished := make(chan ExitCode, 1)
	go func() {
		finished <- Run(t.Context(), instance, []string{inputPath, outputPath}, WithStreams(&stdout, blocked))
	}()
	<-blocked.started
	waitForFile(t, outputPath)
	select {
	case code := <-finished:
		t.Fatalf("CLI returned before joining renderer: %d", code)
	default:
	}
	close(blocked.release)
	if code := <-finished; code != ExitSuccess {
		t.Fatalf("drop exit = %d, stdout=%s stderr=%s", code, stdout.String(), blocked.String())
	}
	if strings.Contains(blocked.String(), "warning observation-loss") {
		t.Fatalf("bounded delivery reported information loss: %s", blocked.String())
	}
}

func TestRunStopsRenderingAfterObservationCleanupTimeout(t *testing.T) {
	instance, err := host.New(host.Plugins(standard.Set()), host.CleanupTimeout(20*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	outputPath := filepath.Join(directory, "output.wav")
	payload := bytes.Repeat([]byte{1, 0, 2, 0}, 4_096)
	if err := os.WriteFile(inputPath, pcmWave(2, 44_100, 16, payload), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := newBlockingWriter()
	var stdout bytes.Buffer
	finished := make(chan ExitCode, 1)
	go func() {
		finished <- Run(t.Context(), instance, []string{inputPath, outputPath}, WithStreams(&stdout, blocked))
	}()
	<-blocked.started
	waitForFile(t, outputPath)
	select {
	case code := <-finished:
		if code != ExitCanceled {
			close(blocked.release)
			t.Fatalf("cleanup timeout exit = %d, want %d", code, ExitCanceled)
		}
	case <-time.After(time.Second):
		close(blocked.release)
		t.Fatal("CLI waited for a renderer past the observation cleanup timeout")
	}
	close(blocked.release)
	<-blocked.finished
}

func TestEventRendererRecoversDeliveryGapFromHistory(t *testing.T) {
	var stderr bytes.Buffer
	renderer := &eventRenderer{destination: &stderr}
	if err := renderer.Emit(t.Context(), host.Event{Sequence: 0, Kind: host.LifecycleEvent}); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Emit(t.Context(), host.Event{Sequence: 2, Kind: host.ProgressEvent}); err != nil {
		t.Fatal(err)
	}
	result := host.Result{
		Events: []host.Event{
			{Sequence: 0, Kind: host.LifecycleEvent},
			{Sequence: 1, Kind: host.LifecycleEvent},
			{Sequence: 2, Kind: host.ProgressEvent},
		},
		Observation: host.ObservationSummary{DeliveryDropped: 1},
	}
	lost, err := renderer.finish(result.Events, result.Observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := renderResult(io.Discard, &stderr, result, nil, lost); err != nil {
		t.Fatal(err)
	}
	if lost || strings.Contains(stderr.String(), "observation-loss") {
		t.Fatalf("recoverable delivery gap reported loss: %s", stderr.String())
	}
	if sequences := renderedSequences(t, stderr.String()); len(sequences) != 3 || sequences[0] != 0 || sequences[1] != 1 || sequences[2] != 2 {
		t.Fatalf("recovered sequences = %v", sequences)
	}
}

func TestEventRendererWarnsWhenHistoryCannotRecoverDeliveryGap(t *testing.T) {
	var stderr bytes.Buffer
	renderer := &eventRenderer{destination: &stderr}
	for _, event := range []host.Event{
		{Sequence: 0, Kind: host.LifecycleEvent},
		{Sequence: 2, Kind: host.ProgressEvent},
	} {
		if err := renderer.Emit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	result := host.Result{
		Events: []host.Event{
			{Sequence: 2, Kind: host.ProgressEvent},
			{Sequence: 3, Kind: host.LifecycleEvent},
		},
		Observation: host.ObservationSummary{HistoryDropped: 2, DeliveryDropped: 1},
	}
	lost, err := renderer.finish(result.Events, result.Observation)
	if err != nil {
		t.Fatal(err)
	}
	if err := renderResult(io.Discard, &stderr, result, nil, lost); err != nil {
		t.Fatal(err)
	}
	if !lost || !strings.Contains(stderr.String(), "warning observation-loss history=2 delivery=1") {
		t.Fatalf("unrecoverable history overflow = %s", stderr.String())
	}
}

func TestHelpUsesInjectedOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(t.Context(), nil, []string{"--help"}, WithStreams(&stdout, &stderr)); code != ExitSuccess || !strings.HasPrefix(stdout.String(), "usage: godec") || stderr.Len() != 0 {
		t.Fatalf("help = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("renderer failed") }

type blockingWriter struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	once     sync.Once
	mu       sync.Mutex
	buffer   bytes.Buffer
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
}

func (w *blockingWriter) Write(value []byte) (int, error) {
	first := false
	w.once.Do(func() {
		first = true
		close(w.started)
		<-w.release
	})
	w.mu.Lock()
	written, err := w.buffer.Write(value)
	w.mu.Unlock()
	if first {
		close(w.finished)
	}
	return written, err
}

func (w *blockingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func waitForFile(t testing.TB, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("output was not committed while renderer was blocked")
}

func renderedSequences(t testing.TB, output string) []uint64 {
	t.Helper()
	var result []uint64
	for _, line := range strings.Split(output, "\n") {
		for _, field := range strings.Fields(line) {
			if !strings.HasPrefix(field, "sequence=") {
				continue
			}
			sequence, err := strconv.ParseUint(strings.TrimPrefix(field, "sequence="), 10, 64)
			if err != nil {
				t.Fatalf("parse sequence from %q: %v", line, err)
			}
			result = append(result, sequence)
			break
		}
	}
	return result
}

func pcmWave(channels uint16, rate uint32, bits uint16, payload []byte) []byte {
	blockAlign := channels * bits / 8
	byteRate := rate * uint32(blockAlign)
	result := make([]byte, 44+len(payload))
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], channels)
	binary.LittleEndian.PutUint32(result[24:28], rate)
	binary.LittleEndian.PutUint32(result[28:32], byteRate)
	binary.LittleEndian.PutUint16(result[32:34], blockAlign)
	binary.LittleEndian.PutUint16(result[34:36], bits)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], uint32(len(payload)))
	copy(result[44:], payload)
	return result
}
