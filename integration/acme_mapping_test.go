package integration_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/integration/acme"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/standard"
)

type (
	acmeWAVEBridgePluginID struct{}
	acmeWAVEBridgeID       struct{}
	acmeWAVEBridgeConfigID struct{}
	acmeWAVEBridgeConfig   struct{}
	acmeWAVEBridgePlan     struct{ shape flow.Shape }
)

func TestThirdPartyMetadataMappingProjectsACMELabelIntoWAVE(t *testing.T) {
	const label = "mapped title"
	inputReference, err := acme.Reference(mustACME(t, label, []byte{1, 2, 3, 4}))
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromReference(inputReference)
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithPort(job.At("reader", "bytes"))
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "mapped.wav")
	output, err := job.OutputToReference(localFileReference(t, outputPath))
	if err != nil {
		t.Fatal(err)
	}
	output, err = output.WithPort(job.At("mux", "writes"))
	if err != nil {
		t.Fatal(err)
	}
	bridgeComponent := acmeWAVEBridgeComponent()
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("reader", acme.ReaderIdentity(), config.NewPatch()),
			job.NewNode("bridge", bridgeComponent.Identity(), config.NewPatch()),
			job.NewNode("mux", wave.MuxerIdentity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("reader", "packets"), job.At("bridge", "in")),
			job.Connect(job.At("bridge", "out"), job.At("mux", "packets")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph)
	if err != nil {
		t.Fatal(err)
	}
	bridge := plugin.Define[acmeWAVEBridgePluginID](plugin.Descriptor{DisplayName: "ACME to WAVE bridge", Version: "1"}, bridgeComponent)
	instance, err := host.New(host.Plugins(standard.Set().Add(acme.Plugin()).Add(bridge)))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Error(err)
		}
	}()

	predicted := prepared.Plan().PredictedMetadataLosses()
	if len(predicted) != 1 {
		t.Fatalf("predicted metadata losses = %#v, want one conversion", predicted)
	}
	if warnings := prepared.Plan().Warnings(); len(warnings) != 0 {
		t.Fatalf("lossless metadata conversion warnings = %v, want none", warnings)
	}
	assertACMEWAVEMappingLoss(t, predicted[0].Output, predicted[0].Node, predicted[0].Component, predicted[0].Port, predicted[0].Report)

	result, err := prepared.Run(t.Context())
	if err != nil || !result.Succeeded() {
		t.Fatalf("Run = %#v, %v", result, err)
	}
	actual := result.ActualMetadataLosses()
	if len(actual) != 1 {
		t.Fatalf("actual metadata losses = %#v, want one committed conversion", actual)
	}
	assertACMEWAVEMappingLoss(t, actual[0].Output, actual[0].Node, actual[0].Component, actual[0].Port, actual[0].Report)
	if actual[0].Report != predicted[0].Report {
		t.Fatalf("actual report = %#v, want predicted %#v", actual[0].Report, predicted[0].Report)
	}
	encoded, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !hasINFOValue(parseRIFF(t, encoded), "INAM", label) {
		t.Fatalf("WAVE INFO does not contain mapped title %q: %x", label, encoded)
	}
}

func assertACMEWAVEMappingLoss(t testing.TB, output int, node, component, port string, report loss.Report) {
	t.Helper()
	if output != 0 || node != "mux" || component != wave.MuxerIdentity().String() || port != "writes" {
		t.Fatalf("metadata loss location = output %d %s %s.%s", output, node, component, port)
	}
	if report.Carrier != wave.RIFFInfo() || report.Encoding != wave.InfoEncodingIdentity().String() || report.Block != "wave/info/0" {
		t.Fatalf("metadata loss target = %#v", report)
	}
	want := loss.Loss{
		Key: acme.Label().ID(), Kind: loss.Converted, Target: tag.Title().ID(), Mapping: loss.Lossless, Detail: "metadata.mapping",
		Source: loss.Origin{Carrier: acme.LabelCarrier(), Encoding: acme.EncodingIdentity().String(), Block: "acme/label", Native: "label"},
	}
	if report.Loss != want {
		t.Fatalf("metadata conversion = %#v, want %#v", report.Loss, want)
	}
}

func hasINFOValue(chunks []riffTestChunk, native, value string) bool {
	for _, chunk := range chunks {
		if string(chunk.id) != "LIST" || !bytes.HasPrefix(chunk.payload, []byte("INFO")) {
			continue
		}
		payload := chunk.payload[4:]
		for len(payload) >= 8 {
			length := int(uint32(payload[4]) | uint32(payload[5])<<8 | uint32(payload[6])<<16 | uint32(payload[7])<<24)
			end := 8 + length
			if end > len(payload) {
				return false
			}
			if string(payload[:4]) == native && string(bytes.TrimRight(payload[8:end], "\x00")) == value {
				return true
			}
			payload = payload[end:]
			if length&1 != 0 {
				if len(payload) == 0 {
					return false
				}
				payload = payload[1:]
			}
		}
	}
	return false
}

func acmeWAVEBridgeComponent() plugin.Component {
	shape := flow.NewShape([]flow.Port{flow.In("in", codec.Packets())}, []flow.Port{flow.Out("out", codec.Packets())})
	schema := config.Struct[acmeWAVEBridgeConfigID](func() acmeWAVEBridgeConfig { return acmeWAVEBridgeConfig{} }).Version("1").Build()
	spec := plugin.Spec[acmeWAVEBridgeConfig, acmeWAVEBridgePlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, _ acmeWAVEBridgeConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[acmeWAVEBridgePlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[acmeWAVEBridgePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("acme-wave-bridge.input")),
				}}, nil
			}
			properties, err := (sample.Description{
				Signal: sample.Signal{Rate: 8_000, Layout: sample.Mono(), ValidBits: 8},
				Coding: sample.U8, Packing: sample.Interleaved, Endian: sample.NoEndian,
			}).Properties()
			if err != nil {
				return plugin.Compiled[acmeWAVEBridgePlan, stream.Descriptor]{}, err
			}
			output, err := stream.NewDescriptor(input.ID(), codec.Packets().Descriptor(), timing.MustBase(1, 8_000), properties)
			if err != nil {
				return plugin.Compiled[acmeWAVEBridgePlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[acmeWAVEBridgePlan, stream.Descriptor]{
				Plan:    acmeWAVEBridgePlan{shape: shape.Clone()},
				Outputs: flow.NewDescriptors(flow.Describe("out", output.WithMetadata(input.Metadata()))),
				Effects: []plugin.Effect{{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "acme-wave-bridge"}},
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan acmeWAVEBridgePlan) (flow.Operator, error) {
			return &acmeWAVEBridgeOperator{shape: plan.shape.Clone()}, nil
		},
	}
	return plugin.NewComponent[acmeWAVEBridgeID](plugin.Descriptor{DisplayName: "ACME to WAVE packets"}, schema,
		plugin.WithSpec(spec), plugin.WithProcessor("in", codec.Packets(), "out", codec.Packets()))
}

type acmeWAVEBridgeOperator struct {
	shape flow.Shape
	out   flow.Item[packet.Packet]
}

func (o *acmeWAVEBridgeOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*acmeWAVEBridgeOperator) Close() error        { return nil }

func (o *acmeWAVEBridgeOperator) Process(ctx context.Context, input *flow.Item[packet.Packet], output flow.Emitter[packet.Packet]) error {
	if !input.Valid() {
		return errors.New("ACME to WAVE bridge received an invalid packet")
	}
	if err := flow.Transfer(input, &o.out, output, func(value packet.Packet) (packet.Packet, error) { return value, nil }); err != nil {
		return err
	}
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

func (*acmeWAVEBridgeOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error { return nil }
