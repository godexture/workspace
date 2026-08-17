package integration_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/linear"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/resource"
	"github.com/godexture/godec/standard"
)

const replayBlockSize = 8

type (
	replayPluginID struct{}
	replaySourceID struct{}
	replayConfigID struct{}
	replayConfig   struct{}
	replayPlan     struct{ shape flow.Shape }
)

type replaySourceState struct {
	payload  []byte
	acquired atomic.Int32
	closed   atomic.Int32
	read     atomic.Int64
}

type replaySourceSession struct {
	state  *replaySourceState
	offset int
}

func (*replaySourceSession) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.SequentialRead)
	return value
}

func (s *replaySourceSession) Read(_ context.Context, destination []byte) (int, error) {
	if s.offset >= len(s.state.payload) {
		return 0, io.EOF
	}
	count := copy(destination, s.state.payload[s.offset:])
	s.offset += count
	s.state.read.Add(int64(count))
	if count != len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *replaySourceSession) Close() error {
	s.state.closed.Add(1)
	return nil
}

type replaySourceOperator struct {
	shape   flow.Shape
	reader  access.Sequential
	buffers *buffer.Allocator
	done    bool
}

func (o *replaySourceOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*replaySourceOperator) Close() error        { return nil }

func (o *replaySourceOperator) Read(ctx context.Context, into *flow.Item[buffer.Handle]) error {
	if o.done {
		return io.EOF
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 1, Planes: []buffer.PlaneSpec{{Size: replayBlockSize}}})
	if err != nil {
		return err
	}
	defer lease.Discard()
	count := 0
	eof := false
	err = lease.Fill(func(value buffer.Mutable) error {
		destination := value.Bytes()
		for count < len(destination) {
			read, readErr := o.reader.Read(ctx, destination[count:])
			if read < 0 || read > len(destination)-count {
				return errors.New("sequential fixture returned an invalid read count")
			}
			count += read
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					eof = true
					return nil
				}
				return readErr
			}
			if read == 0 {
				return io.ErrNoProgress
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 && eof {
		o.done = true
		return io.EOF
	}
	payload, err := lease.Commit()
	if err != nil {
		return err
	}
	if count != replayBlockSize {
		exact, rangeErr := payload.Range(0, count)
		payload.Release()
		if rangeErr != nil {
			return rangeErr
		}
		payload = exact
	}
	o.done = eof
	into.Set(payload)
	return nil
}

func TestSequentialAutomaticRawFallbackReplaysProbePrefixEndToEnd(t *testing.T) {
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	for _, preset := range []job.Preset{job.Fast, job.Realtime} {
		t.Run(preset.String(), func(t *testing.T) {
			state := &replaySourceState{payload: append([]byte(nil), payload...)}
			set := standard.Set().Add(replaySourcePlugin(state))
			instance, err := host.New(
				host.Plugins(set),
				host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
			)
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			outputPath := filepath.Join(directory, "output.pcm")
			inputReference, _ := access.Parse("sequence:input")
			input, _ := job.InputFromReference(inputReference)
			output, _ := job.OutputToReference(localFileReference(t, outputPath))
			request := automaticPCMRequest(t, preset, input, output)
			prepared, err := instance.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if state.acquired.Load() != 1 || state.read.Load() != 12 || state.closed.Load() != 0 {
				t.Fatalf("after Prepare: acquired=%d, read=%d, closed=%d", state.acquired.Load(), state.read.Load(), state.closed.Load())
			}
			assertRawFallbackPlan(t, prepared.Plan())
			result, err := prepared.Run(t.Context())
			if err != nil || !result.Succeeded() {
				t.Fatalf("Run result = %#v, error=%v", result, err)
			}
			if err := prepared.Close(); err != nil {
				t.Fatal(err)
			}
			if state.acquired.Load() != 1 || state.read.Load() != int64(len(payload)) || state.closed.Load() != 1 {
				t.Fatalf("after Run: acquired=%d, read=%d, closed=%d", state.acquired.Load(), state.read.Load(), state.closed.Load())
			}
			outputBytes, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(outputBytes, payload) {
				t.Fatalf("raw PCM = %x, want %x", outputBytes, payload)
			}
		})
	}
}

func TestSequentialWAVEMatchDoesNotFallBackToRaw(t *testing.T) {
	payload := riffWAVE([]byte{1, 0, 2, 0}, 1, 48_000, 16)
	state := &replaySourceState{payload: payload}
	instance, err := host.New(
		host.Plugins(standard.Set().Add(replaySourcePlugin(state))),
		host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("sequence:input")
	input, _ := job.InputFromReference(inputReference)
	outputPath := filepath.Join(t.TempDir(), "output.pcm")
	output, _ := job.OutputToReference(localFileReference(t, outputPath))
	request := automaticPCMRequest(t, job.Fast, input, output)
	_, err = instance.Prepare(t.Context(), request)
	items := host.Diagnostics(err)
	if len(items) != 1 || items[0].Code != "bind.capability-unsatisfied" || items[0].Detail["format"] != wave.WAVE().Identity().String() {
		t.Fatalf("diagnostic = %#v, error=%v", items, err)
	}
	if state.acquired.Load() != 1 || state.read.Load() != 12 || state.closed.Load() != 1 {
		t.Fatalf("failed selection resources = acquired %d, read %d, closed %d", state.acquired.Load(), state.read.Load(), state.closed.Load())
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed input selection touched output: %v", statErr)
	}
}

func TestSequentialRawFallbackPlanClosesProbeSession(t *testing.T) {
	state := &replaySourceState{payload: []byte("0123456789abcdef")}
	instance, err := host.New(
		host.Plugins(standard.Set().Add(replaySourcePlugin(state))),
		host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("sequence:input")
	input, _ := job.InputFromReference(inputReference)
	outputPath := filepath.Join(t.TempDir(), "output.pcm")
	output, _ := job.OutputToReference(localFileReference(t, outputPath))
	planned, err := instance.Plan(t.Context(), automaticPCMRequest(t, job.Fast, input, output))
	if err != nil {
		t.Fatal(err)
	}
	assertRawFallbackPlan(t, planned)
	if state.acquired.Load() != 1 || state.read.Load() != 12 || state.closed.Load() != 1 {
		t.Fatalf("Plan resources = acquired %d, read %d, closed %d", state.acquired.Load(), state.read.Load(), state.closed.Load())
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Plan touched output: %v", statErr)
	}
}

func TestMalformedWAVEMatchDoesNotFallBackToRaw(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "malformed.wav")
	outputPath := filepath.Join(directory, "output.pcm")
	if err := os.WriteFile(inputPath, []byte("RIFF\x04\x00\x00\x00WAVE"), 0o600); err != nil {
		t.Fatal(err)
	}
	instance, err := host.New(
		host.Plugins(standard.Set()),
		host.PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := job.InputFromReference(localFileReference(t, inputPath))
	output, _ := job.OutputToReference(localFileReference(t, outputPath))
	_, err = instance.Prepare(t.Context(), automaticPCMRequest(t, job.Fast, input, output))
	if !errors.Is(err, wave.ErrMalformed) {
		t.Fatalf("malformed WAVE error = %v", err)
	}
	if strings.Contains(err.Error(), "format.fallback") {
		t.Fatalf("malformed WAVE was downgraded to raw fallback: %v", err)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("malformed input touched output: %v", statErr)
	}
}

func automaticPCMRequest(t *testing.T, preset job.Preset, input job.Input, output job.Output) job.Job {
	t.Helper()
	patch := config.NewPatch().
		SetText("rate", strconv.Itoa(48_000)).
		SetText("validBits", "16").
		SetText("layout", "mono").
		SetText("endian", "little").
		SetText("chunkSamples", "1024")
	hint, err := job.SelectFormat(linear.Raw())
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithFormatHint(hint.WithConfig(patch))
	if err != nil {
		t.Fatal(err)
	}
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("parser", linear.ParserIdentity(), patch),
			job.NewNode("decoder", linear.DecoderIdentity(), patch),
			job.NewNode("encoder", linear.EncoderIdentity(), patch),
			job.NewNode("writer", linear.WriterIdentity(), patch),
		},
		[]job.Edge{
			job.Connect(job.At("parser", "packets"), job.At("decoder", "packets")),
			job.Connect(job.At("decoder", "frames"), job.At("encoder", "frames")),
			job.Connect(job.At("encoder", "packets"), job.At("writer", "packets")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy, ok := job.PolicyFor(preset)
	if !ok {
		t.Fatalf("policy %s is unavailable", preset)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, requested, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func replaySourcePlugin(state *replaySourceState) plugin.Definition {
	configuration := config.Struct[replayConfigID](func() replayConfig { return replayConfig{} }).Version("1").Build()
	shape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", access.Bytes())})
	spec := plugin.Spec[replayConfig, replayPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(plugin.CompileContext, replayConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[replayPlan, stream.Descriptor], error) {
			descriptor, err := stream.NewDescriptor("sequence", access.Bytes().Descriptor(), timing.Base{}, property.New())
			if err != nil {
				return plugin.Compiled[replayPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[replayPlan, stream.Descriptor]{
				Plan:      replayPlan{shape: shape.Clone()},
				Outputs:   flow.NewDescriptors(flow.Describe("bytes", descriptor)),
				Resources: resource.Request{Memory: replayBlockSize},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, value replayPlan) (flow.Operator, error) {
			opening, ok := plugin.Boundary[access.Opening](ctx)
			if !ok || ctx.Buffers() == nil {
				return nil, errors.New("sequential fixture requires a prepared opening and buffer grant")
			}
			reader, ok := access.SequentialOf(opening)
			if !ok {
				return nil, errors.New("sequential fixture requires sequential reading")
			}
			return &replaySourceOperator{shape: value.shape.Clone(), reader: reader, buffers: ctx.Buffers()}, nil
		},
	}
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	component := plugin.NewComponent[replaySourceID](plugin.Descriptor{DisplayName: "Sequential fixture"}, configuration,
		plugin.WithSpec(spec),
		plugin.WithReader("bytes", access.Bytes()),
		access.Source("sequence", capabilities, func(context.Context, access.Reference, access.Selection) (access.Session, error) {
			state.acquired.Add(1)
			return &replaySourceSession{state: state}, nil
		}),
	)
	return plugin.Define[replayPluginID](plugin.Descriptor{DisplayName: "Sequential fixture", Version: "1"}, component)
}

func assertRawFallbackPlan(t *testing.T, value plan.Plan) {
	t.Helper()
	warnings := value.Warnings()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "without content evidence") || !strings.Contains(warnings[0], linear.Raw().Identity().String()) {
		t.Fatalf("fallback warnings = %v", warnings)
	}
	usage := value.Usage()
	if usage.ProbeBytes != 12 || usage.ProbeRounds != 2 {
		t.Fatalf("fallback Probe usage = %#v", usage)
	}
	found := false
	for _, node := range value.Nodes() {
		if node.Component != linear.ReaderIdentity().String() {
			continue
		}
		found = true
		if node.Origin != plan.Automatic || node.Reason != "format.fallback" {
			t.Fatalf("raw fallback node = %#v", node)
		}
		planned := make(map[string]bool)
		for _, field := range node.Config.Fields() {
			planned[field.ID] = field.Source == config.SourcePlanner
		}
		for _, field := range []string{"rate", "validBits", "layout", "endian"} {
			if !planned[field] {
				t.Fatalf("raw fallback config %q is not planner-derived: %#v", field, node.Config.Fields())
			}
		}
	}
	if !found {
		t.Fatal("raw fallback reader is absent from Plan")
	}
	for _, boundary := range value.Boundaries() {
		if boundary.Direction == plan.InputBoundary && (len(boundary.Selected) != 1 || boundary.Selected[0] != access.SequentialRead) {
			t.Fatalf("fallback input selection = %#v", boundary)
		}
	}
}
