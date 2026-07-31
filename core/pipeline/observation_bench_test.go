package pipeline

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/godexture/godec/core/domain/media"
)

func BenchmarkPipelineObservation(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"1MiB", 1 << 20}, {"64MiB", 64 << 20}} {
		for _, mode := range []struct {
			name  string
			mode  ObservationMode
			plain bool
		}{{name: "Plain", plain: true}, {name: "Off", mode: ObservationOff}, {name: "Progress", mode: ObservationProgress}, {name: "Metrics", mode: ObservationMetrics}} {
			b.Run(fmt.Sprintf("%s/%s", size.name, mode.name), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size.bytes))
				for b.Loop() {
					var conversion *Pipeline
					var err error
					if mode.plain {
						conversion, err = plainObservationPipeline(size.bytes/4096, 4096)
					} else {
						conversion, _, err = observationPipeline(mode.mode, size.bytes/4096, 4096, false)
					}
					if err != nil {
						b.Fatal(err)
					}
					if err := conversion.Run(context.Background()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkObservedEdgeTransfer(b *testing.B) {
	packet := media.NewPacket(4096)
	defer packet.Release()
	packet.PTS = 4096
	packet.Timebase = observationTimebase
	frame := media.NewAudioFrame(media.SampleFormatS16, media.LayoutStereo2_0, 48000, 1024, media.WithAudioPts(1024))
	defer frame.Release()
	var wrapped media.Frame = frame
	ctx := context.Background()

	b.Run("Packet/Plain", func(b *testing.B) {
		edge := NewChanEdge[*media.Packet](1)
		b.ReportAllocs()
		for b.Loop() {
			if err := edge.Push(ctx, packet); err != nil {
				b.Fatal(err)
			}
			if _, err := edge.Pull(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Packet/Progress", func(b *testing.B) {
		edge := &progressEdge[*media.Packet]{ChanEdge: NewChanEdge[*media.Packet](1), metrics: &edgeMetrics{}}
		b.ReportAllocs()
		for b.Loop() {
			if err := edge.Push(ctx, packet); err != nil {
				b.Fatal(err)
			}
			if _, err := edge.Pull(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Packet/Metrics", func(b *testing.B) {
		edge := &observedEdge[*media.Packet]{ChanEdge: NewChanEdge[*media.Packet](1), metrics: &edgeMetrics{}}
		b.ReportAllocs()
		for b.Loop() {
			if err := edge.Push(ctx, packet); err != nil {
				b.Fatal(err)
			}
			if _, err := edge.Pull(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Frame/Plain", func(b *testing.B) {
		edge := NewChanEdge[media.Frame](1)
		b.ReportAllocs()
		for b.Loop() {
			if err := edge.Push(ctx, wrapped); err != nil {
				b.Fatal(err)
			}
			if _, err := edge.Pull(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Frame/Progress", func(b *testing.B) {
		edge := &progressEdge[media.Frame]{ChanEdge: NewChanEdge[media.Frame](1), metrics: &edgeMetrics{}}
		b.ReportAllocs()
		for b.Loop() {
			if err := edge.Push(ctx, wrapped); err != nil {
				b.Fatal(err)
			}
			if _, err := edge.Pull(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Frame/Metrics", func(b *testing.B) {
		edge := &observedEdge[media.Frame]{ChanEdge: NewChanEdge[media.Frame](1), metrics: &edgeMetrics{}}
		b.ReportAllocs()
		for b.Loop() {
			if err := edge.Push(ctx, wrapped); err != nil {
				b.Fatal(err)
			}
			if _, err := edge.Pull(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkPipelineObservationPaired64MiB(b *testing.B) {
	const totalBytes = 64 << 20
	type pair struct {
		first  string
		second string
		ratios []float64
	}
	pairs := []pair{
		{first: "plain", second: "off"},
		{first: "off", second: "progress"},
		{first: "off", second: "metrics"},
	}
	sample := 0
	for b.Loop() {
		for i := range pairs {
			first, second := pairs[i].first, pairs[i].second
			if sample%2 == 1 {
				first, second = second, first
			}
			firstDuration := benchmarkObservationVariant(b, first, totalBytes)
			secondDuration := benchmarkObservationVariant(b, second, totalBytes)
			durations := map[string]time.Duration{first: firstDuration, second: secondDuration}
			pairs[i].ratios = append(pairs[i].ratios,
				float64(durations[pairs[i].second])/float64(durations[pairs[i].first]))
		}
		sample++
	}
	b.ReportMetric((medianRatio(pairs[0].ratios)-1)*100, "off-vs-plain-%")
	b.ReportMetric((medianRatio(pairs[1].ratios)-1)*100, "progress-vs-off-%")
	b.ReportMetric((medianRatio(pairs[2].ratios)-1)*100, "metrics-vs-off-%")
}

func benchmarkObservationVariant(b *testing.B, variant string, totalBytes int) time.Duration {
	b.Helper()
	started := time.Now()
	var conversion *Pipeline
	var err error
	if variant == "plain" {
		conversion, err = plainObservationPipeline(totalBytes/4096, 4096)
	} else {
		mode := map[string]ObservationMode{
			"off": ObservationOff, "progress": ObservationProgress, "metrics": ObservationMetrics,
		}[variant]
		conversion, _, err = observationPipeline(mode, totalBytes/4096, 4096, false)
	}
	if err != nil {
		b.Fatal(err)
	}
	if err := conversion.Run(context.Background()); err != nil {
		b.Fatal(err)
	}
	return time.Since(started)
}

func medianRatio(values []float64) float64 {
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}
