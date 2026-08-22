package integration_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/standard"
)

// The M7 fast path is MP4 direct demux, per-track copy or a typed PCM path,
// SerialFanIn, MP4 mux. Prepare and Run are separated so a regression can be
// attributed to route and mapping construction or to the steady state, and both
// are reported with allocations. Compare variants by running the pair AB/BA in
// one process; a single timing against another machine or power state is not a
// comparison.

func BenchmarkMP4RemuxPrepare(b *testing.B) {
	benchmarkPrepare(b, mp4BenchmarkJob(b, "remux.mp4", "remuxed.mp4", mp4TwoTrackFixture()))
}

func BenchmarkMP4RemuxRun(b *testing.B) {
	benchmarkRun(b, mp4BenchmarkJob(b, "remux.mp4", "remuxed.mp4", mp4TwoTrackFixture()))
}

func BenchmarkMP4PCMBoundPrepare(b *testing.B) {
	benchmarkPrepare(b, mp4BenchmarkJob(b, "pcm.mp4", "pcm.wav", mp4BenchmarkPCMFixture()))
}

func BenchmarkMP4PCMBoundRun(b *testing.B) {
	benchmarkRun(b, mp4BenchmarkJob(b, "pcm.mp4", "pcm.wav", mp4BenchmarkPCMFixture()))
}

func benchmarkPrepare(b *testing.B, request job.Job) {
	instance := mp4BenchmarkHost(b)
	b.ReportAllocs()
	for b.Loop() {
		prepared, err := instance.Prepare(b.Context(), request)
		if err != nil {
			b.Fatal(err)
		}
		if err := prepared.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRun(b *testing.B, request job.Job) {
	instance := mp4BenchmarkHost(b)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		prepared, err := instance.Prepare(b.Context(), request)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		result, runErr := prepared.Run(b.Context())
		b.StopTimer()
		if runErr != nil || !result.Succeeded() {
			b.Fatalf("MP4 benchmark Run = %#v, %v", result, runErr)
		}
		if err := prepared.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func mp4BenchmarkHost(b *testing.B) *host.Host {
	b.Helper()
	instance, err := standard.NewHost()
	if err != nil {
		b.Fatal(err)
	}
	return instance
}

func mp4BenchmarkJob(b *testing.B, input, output string, source []byte) job.Job {
	b.Helper()
	directory := b.TempDir()
	inputPath := filepath.Join(directory, input)
	if err := os.WriteFile(inputPath, source, 0o600); err != nil {
		b.Fatal(err)
	}
	request, err := standard.NewFileJob(inputPath, filepath.Join(directory, output))
	if err != nil {
		b.Fatal(err)
	}
	return request
}

// mp4BenchmarkPCMFixture is big-endian, so the output format cannot copy it and
// the planner has to bind the PCM decoder and encoder.
func mp4BenchmarkPCMFixture() []byte {
	const frames = 4096
	payload := make([]byte, frames*4)
	for index := range frames * 2 {
		binary.BigEndian.PutUint16(payload[index*2:], uint16(index))
	}
	return mp4PCMFixture("twos", payload)
}
