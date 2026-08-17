package integration_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/standard"
)

type (
	countingMP4PluginID    struct{}
	countingMP4SourceID    struct{}
	countingMP4ConfigID    struct{}
	countingMP4Config      struct{}
	countingMP4Plan        struct{ shape flow.Shape }
	countingMP4SourceState struct {
		data     []byte
		acquires atomic.Int32
		opens    atomic.Int32
		session  *countingMP4Session
	}
)

type countingMP4Session struct {
	data   []byte
	reads  atomic.Int64
	bytes  atomic.Int64
	closed atomic.Int32
}

func (s *countingMP4Session) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.RandomRead, access.StableSize)
	return value
}

func (s *countingMP4Session) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	s.reads.Add(1)
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[offset:])
	s.bytes.Add(int64(count))
	if count != len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *countingMP4Session) Size(context.Context) (int64, error) {
	return int64(len(s.data)), nil
}

func (*countingMP4Session) Snapshot(context.Context) (access.Snapshot, error) {
	return access.NewSnapshot("counting-mp4/fixture/v1", access.StrongSnapshot)
}

func (s *countingMP4Session) Close() error {
	s.closed.Add(1)
	return nil
}

func countingMP4Source(state *countingMP4SourceState) plugin.Component {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", access.Bytes())})
	schema := config.Struct[countingMP4ConfigID](func() countingMP4Config { return countingMP4Config{} }).Version("1").Build()
	capabilities, _ := access.NewCapabilities(access.RandomRead, access.StableSize)
	return plugin.NewComponent[countingMP4SourceID](plugin.Descriptor{DisplayName: "Counting MP4 source"}, schema,
		plugin.WithSpec(plugin.Spec[countingMP4Config, countingMP4Plan, stream.Descriptor]{
			Ports: shape,
			Compile: func(plugin.CompileContext, countingMP4Config, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[countingMP4Plan, stream.Descriptor], error) {
				descriptor := stream.MustDescriptor("counting-mp4", access.Bytes().Descriptor(), timing.Base{}, property.New())
				return plugin.Compiled[countingMP4Plan, stream.Descriptor]{
					Plan:    countingMP4Plan{shape: shape.Clone()},
					Outputs: flow.NewDescriptors(flow.Describe("bytes", descriptor)),
				}, nil
			},
			Open: func(plugin.OpenContext, countingMP4Plan) (flow.Operator, error) {
				state.opens.Add(1)
				return nil, errors.New("counting MP4 carrier must not open")
			},
		}),
		plugin.WithReader("bytes", access.Bytes()),
		access.Source("counting-mp4", capabilities, func(context.Context, access.Reference, access.Selection) (access.Session, error) {
			state.acquires.Add(1)
			state.session = &countingMP4Session{data: state.data}
			return state.session, nil
		}),
	)
}

func TestMP4DirectReaderDoesNotScanTheCarrierDuringRun(t *testing.T) {
	inputBytes := mp4PayloadHeavyFixture(512 * 1024)
	state := &countingMP4SourceState{data: inputBytes}
	definition := plugin.Define[countingMP4PluginID](
		plugin.Descriptor{DisplayName: "MP4 I/O fixture", Version: "1"},
		countingMP4Source(state),
	)
	instance, err := standard.NewHost(definition)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := access.Parse("counting-mp4:fixture")
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromReference(reference)
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "output.mp4")
	request := newMP4RemuxJobFromInput(t, input, outputPath)
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if state.acquires.Load() != 1 || state.opens.Load() != 0 || state.session == nil || state.session.bytes.Load() == 0 {
		t.Fatalf("MP4 Prepare lifecycle = acquire %d, carrier open %d, session %#v, bytes %d", state.acquires.Load(), state.opens.Load(), state.session, state.session.bytes.Load())
	}
	state.session.reads.Store(0)
	state.session.bytes.Store(0)
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("MP4 direct Run = %#v, %v", result, runErr)
	}
	readBytes := state.session.bytes.Load()
	if readBytes < 512*1024 {
		t.Fatalf("MP4 Run read only %d bytes, want at least every sample payload", readBytes)
	}
	if maximum := int64(len(inputBytes)) * 5 / 4; readBytes > maximum {
		t.Fatalf("MP4 Run read %d bytes for a %d-byte source, want at most %d without a carrier scan", readBytes, len(inputBytes), maximum)
	}
	if state.session.reads.Load() == 0 || state.acquires.Load() != 1 || state.opens.Load() != 0 {
		t.Fatalf("MP4 Run lifecycle = reads %d, acquire %d, carrier open %d", state.session.reads.Load(), state.acquires.Load(), state.opens.Load())
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if state.session.closed.Load() != 1 {
		t.Fatalf("MP4 source closes = %d, want one shared session", state.session.closed.Load())
	}
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outputBytes, inputBytes) {
		t.Fatal("MP4 direct I/O fixture was not remuxed exactly")
	}
}

func newMP4RemuxJobFromInput(t testing.TB, input job.Input, outputPath string) job.Job {
	t.Helper()
	output, err := job.OutputToReference(localFileReference(t, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("demux", mp4.DemuxerIdentity(), config.NewPatch()),
			job.NewNode("mux", mp4.MuxerIdentity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("demux", "packets"), job.At("mux", "packets"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := job.PolicyFor(job.Fast)
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func mp4PayloadHeavyFixture(payloadBytes int) []byte {
	payload := make([]byte, payloadBytes)
	for index := range payload {
		payload[index] = byte(index*31 + 7)
	}
	tracks := []mp4FixtureTrack{{id: 1, timeScale: 90_000, handler: "vide", entry: "zzzz", duration: 3_000, payload: payload}}
	fileTypePayload := append([]byte("isom"), mp4FixtureU32(0)...)
	fileTypePayload = append(fileTypePayload, []byte("iso2")...)
	fileType := mp4FixtureBox("ftyp", fileTypePayload)
	moov := mp4FixtureMoov(tracks)
	tracks[0].offset = int32(len(fileType) + len(moov) + 8)
	moov = mp4FixtureMoov(tracks)
	return append(append(fileType, moov...), mp4FixtureBox("mdat", payload)...)
}
