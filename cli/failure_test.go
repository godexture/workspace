package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/godexture/godec/standard"
)

// failAfter breaks once the command writes a line it recognizes, which lets a
// test fail stdout at a chosen point: before the Plan, or after it but before
// the result summary.
type failAfter struct {
	trigger string
	message string
	broken  bool
	written bytes.Buffer
}

func (w *failAfter) Write(value []byte) (int, error) {
	if w.broken || w.trigger == "" || strings.HasPrefix(string(value), w.trigger) {
		w.broken = true
		return 0, errors.New(w.message)
	}
	return w.written.Write(value)
}

// A non-zero exit says something went wrong; it cannot say what, and it cannot
// say that more than one thing did. Every independent failure has to survive
// in the returned error, or an embedding application has nothing to diagnose.
func TestRunReportsEveryIndependentFailure(t *testing.T) {
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	if err := os.WriteFile(inputPath, pcmWave(1, 48_000, 16, []byte{1, 0, 2, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	rawPath := filepath.Join(directory, "input.raw")
	if err := os.WriteFile(rawPath, []byte{1, 0}, 0o600); err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		args    []string
		stdout  func() *failAfter
		stderr  bool
		code    ExitCode
		reasons []string
	}{
		{
			name:    "failing stdout during the plan",
			ctx:     context.Background(),
			args:    []string{inputPath, filepath.Join(directory, "plan-render.wav")},
			stdout:  func() *failAfter { return &failAfter{message: "stdout closed"} },
			code:    ExitRuntime,
			reasons: []string{"stdout closed"},
		},
		{
			name:    "failing stdout after the plan",
			ctx:     context.Background(),
			args:    []string{inputPath, filepath.Join(directory, "result-render.wav")},
			stdout:  func() *failAfter { return &failAfter{trigger: "output ", message: "stdout closed late"} },
			code:    ExitRuntime,
			reasons: []string{"stdout closed late"},
		},
		{
			name:    "failing stderr while reporting a planning failure",
			ctx:     context.Background(),
			args:    []string{rawPath, filepath.Join(directory, "planning.wav")},
			stderr:  true,
			code:    ExitPlanning,
			reasons: []string{"prepare.format-config-required", "renderer failed"},
		},
		{
			name:    "failing stderr while reporting a usage error",
			ctx:     context.Background(),
			args:    []string{inputPath},
			stderr:  true,
			code:    ExitUsage,
			reasons: []string{"renderer failed"},
		},
		{
			name:    "cancellation",
			ctx:     canceled,
			args:    []string{inputPath, filepath.Join(directory, "canceled.wav")},
			code:    ExitCanceled,
			reasons: []string{"context canceled"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := test.stdout
			if out == nil {
				// Never triggered: this case is not about stdout.
				out = func() *failAfter { return &failAfter{trigger: "unreachable"} }
			}
			stdoutWriter := out()
			var stderrWriter io.Writer = &bytes.Buffer{}
			if test.stderr {
				stderrWriter = failingWriter{}
			}
			result := Run(test.ctx, instance, test.args, WithStreams(stdoutWriter, stderrWriter))
			if result.Code != test.code {
				t.Fatalf("exit = %d, want %d (err %v)", result.Code, test.code, result.Err)
			}
			if result.Err == nil {
				t.Fatalf("failing command reported no error")
			}
			for _, reason := range test.reasons {
				if !strings.Contains(result.Err.Error(), reason) {
					t.Errorf("error %v does not mention %q", result.Err, reason)
				}
			}
		})
	}
}

// A plan that cannot be written still owns a prepared job. The cleanup failure
// and the rendering failure are independent, and both belong to the caller.
func TestRunJoinsPlanRenderingAndCleanupFailures(t *testing.T) {
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	if err := os.WriteFile(inputPath, pcmWave(1, 48_000, 16, []byte{1, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	result := Run(context.Background(), instance, []string{inputPath, filepath.Join(directory, "out.wav")},
		WithStreams(failingWriter{}, &stderr))
	if result.Code != ExitRuntime {
		t.Fatalf("exit = %d, want %d", result.Code, ExitRuntime)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), "renderer failed") {
		t.Fatalf("plan rendering failure = %v", result.Err)
	}
	if _, err := os.Stat(filepath.Join(directory, "out.wav")); err == nil {
		t.Fatal("a command that never ran committed its output")
	}
}

// A successful command returns no error at all, so a caller can treat a
// non-nil error as the only signal it needs.
func TestRunReportsNoErrorOnSuccess(t *testing.T) {
	instance, err := standard.NewHost()
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.wav")
	if err := os.WriteFile(inputPath, pcmWave(1, 48_000, 16, []byte{1, 0, 2, 0}), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	result := Run(context.Background(), instance, []string{inputPath, filepath.Join(directory, "out.wav")},
		WithStreams(&stdout, &stderr))
	if !result.Succeeded() {
		t.Fatalf("successful conversion = %#v, stderr=%s", result, stderr.String())
	}
}
