package pipeline

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	mediatime "github.com/godexture/godec/core/domain/time"
	"github.com/godexture/godec/core/node"
)

type observationSource struct {
	out        map[string]*node.OutPort[*media.Packet]
	packets    int
	packetSize int
	event      bool
	block      <-chan struct{}
}

var observationTimebase = mediatime.NewRational(1, 48000)

func newObservationSource(packets, packetSize int, stream media.StreamInfo) *observationSource {
	return &observationSource{
		out:     map[string]*node.OutPort[*media.Packet]{"out": node.NewOutPort[*media.Packet]("out", stream)},
		packets: packets, packetSize: packetSize,
	}
}

func (source *observationSource) OutputPorts() map[string]*node.OutPort[*media.Packet] {
	return source.out
}
func (source *observationSource) Close() error { return nil }
func (source *observationSource) Start(ctx context.Context) error {
	defer source.out["out"].Edge().Close()
	if source.block != nil {
		select {
		case <-source.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for i := range source.packets {
		packet := media.NewPacket(source.packetSize)
		packet.PTS = media.Pts(i * source.packetSize)
		packet.Timebase = observationTimebase
		if err := source.out["out"].Push(ctx, packet); err != nil {
			packet.Release()
			return err
		}
	}
	if source.event {
		return source.out["out"].Push(ctx, media.NewPacketEvent(media.PacketKindStreamEnd, 0, nil))
	}
	return nil
}

type observationSink struct {
	in    map[string]*node.InPort[*media.Packet]
	count int
}

func newObservationSink() *observationSink {
	return &observationSink{in: map[string]*node.InPort[*media.Packet]{"in": node.NewInPort[*media.Packet]("in")}}
}

func (sink *observationSink) InputPorts() map[string]*node.InPort[*media.Packet] { return sink.in }
func (sink *observationSink) Close() error                                       { return nil }
func (sink *observationSink) Start(ctx context.Context) error {
	for {
		packet, err := sink.in["in"].Pull(ctx)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		sink.count++
		packet.Release()
	}
}

func observationPipeline(mode ObservationMode, packets, packetSize int, event bool) (*Pipeline, *observationSource, error) {
	stream := media.StreamInfo{Index: 0, Type: media.MediaAudio}
	source := newObservationSource(packets, packetSize, stream)
	source.event = event
	sink := newObservationSink()
	geometry := NewGeometry()
	if err := geometry.AddNodeDef(NodeDef{ID: "source", Node: source, Description: NodeDescription{Outputs: []media.StreamInfo{stream}}}); err != nil {
		return nil, nil, err
	}
	if err := geometry.AddNodeDef(NodeDef{ID: "sink", Node: sink, Description: NodeDescription{Inputs: []media.StreamInfo{stream}}}); err != nil {
		_ = geometry.Close()
		return nil, nil, err
	}
	if err := geometry.AddEdgeDef(EdgeDef{FromNode: "source", FromPort: "out", ToNode: "sink", ToPort: "in", Stream: stream, ProgressSource: true}); err != nil {
		_ = geometry.Close()
		return nil, nil, err
	}
	conversion, err := NewBuilder().Build(geometry, WithObservation(mode))
	return conversion, source, err
}

func plainObservationPipeline(packets, packetSize int) (*Pipeline, error) {
	stream := media.StreamInfo{Index: 0, Type: media.MediaAudio}
	source := newObservationSource(packets, packetSize, stream)
	sink := newObservationSink()
	if err := LinkWithBufferSize(source, "out", sink, "in", 100); err != nil {
		return nil, err
	}
	return New(source, sink)
}

func TestPipelineObservationModes(t *testing.T) {
	for _, test := range []struct {
		name  string
		mode  ObservationMode
		items uint64
		bytes uint64
	}{
		{name: "off", mode: ObservationOff},
		{name: "progress", mode: ObservationProgress, items: 5},
		{name: "metrics", mode: ObservationMetrics, items: 5, bytes: 5 * 128},
	} {
		t.Run(test.name, func(t *testing.T) {
			conversion, source, err := observationPipeline(test.mode, 5, 128, false)
			if err != nil {
				t.Fatal(err)
			}
			if test.mode == ObservationOff {
				if _, ok := source.out["out"].Edge().(*ChanEdge[*media.Packet]); !ok {
					t.Fatalf("off edge = %T, want *ChanEdge", source.out["out"].Edge())
				}
			}
			if err := conversion.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			snapshot := conversion.Snapshot()
			if got := snapshot.Edges[0].Items; got != test.items {
				t.Fatalf("items = %d, want %d", got, test.items)
			}
			if got := snapshot.Edges[0].Bytes; got != test.bytes {
				t.Fatalf("bytes = %d, want %d", got, test.bytes)
			}
			if test.mode == ObservationMetrics && snapshot.Nodes[0].State != "completed" {
				t.Fatalf("node state = %s", snapshot.Nodes[0].State)
			}
		})
	}
}

func TestMetricsObservationAcceptsPacketEvents(t *testing.T) {
	conversion, _, err := observationPipeline(ObservationMetrics, 1, 16, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := conversion.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	snapshot := conversion.Snapshot()
	if got, want := snapshot.Edges[0].Items, uint64(2); got != want {
		t.Fatalf("items = %d, want %d", got, want)
	}
	if got, want := snapshot.Edges[0].Bytes, uint64(16); got != want {
		t.Fatalf("bytes = %d, want %d", got, want)
	}
}

func TestSnapshotIsSafeWhilePipelineRuns(t *testing.T) {
	conversion, source, err := observationPipeline(ObservationMetrics, 4, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	block := make(chan struct{})
	source.block = block
	done := make(chan error, 1)
	go func() { done <- conversion.Run(context.Background()) }()

	for conversion.Snapshot().State == "ready" {
	}
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				snapshot := conversion.Snapshot()
				if len(snapshot.Nodes) != 2 || len(snapshot.Edges) != 1 {
					t.Errorf("snapshot dimensions = %d nodes, %d edges", len(snapshot.Nodes), len(snapshot.Edges))
					return
				}
			}
		}()
	}
	wait.Wait()
	close(block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMetricsSnapshotAfterCancellation(t *testing.T) {
	conversion, source, err := observationPipeline(ObservationMetrics, 1, 16, false)
	if err != nil {
		t.Fatal(err)
	}
	block := make(chan struct{})
	source.block = block
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- conversion.Run(ctx) }()
	for conversion.Snapshot().State == "ready" {
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	snapshot := conversion.Snapshot()
	if snapshot.State != "closed" {
		t.Fatalf("snapshot state = %q, want closed", snapshot.State)
	}
	if snapshot.Nodes[0].State != "failed" {
		t.Fatalf("source state = %q, want failed", snapshot.Nodes[0].State)
	}
}
