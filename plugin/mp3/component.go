package mp3

import (
	"errors"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/mp3/internal/frame"
	"github.com/godexture/godec/resource"
)

type readerPlan struct {
	shape      flow.Shape
	inspection inspection
}

type parserPlan struct {
	shape      flow.Shape
	framing    framing
	hasFraming bool
	signal     sample.Signal
	maxFrame   int
}

func readerShape() flow.Shape {
	return flow.NewShape(
		[]flow.Port{flow.In("bytes", access.Bytes())},
		[]flow.Port{flow.Out("chunks", format.Chunks())},
	)
}

func parserShape() flow.Shape {
	return flow.NewShape(
		[]flow.Port{flow.In("chunks", format.Chunks())},
		[]flow.Port{flow.Out("packets", codec.Packets())},
	)
}

func readerComponent() plugin.Component {
	shape := readerShape()
	spec := plugin.Spec[configuration, readerPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(ctx plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[readerPlan, stream.Descriptor], error) {
			input, ok := inputs.One("bytes")
			if !ok {
				return plugin.Compiled[readerPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("mp3.input"))}}, nil
			}
			inspected, ok := format.InspectionOf[inspection](ctx, MP3())
			if !ok || !inspected.valid() {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem("mp3.inspection", diagnostic.ErrorSeverity, diagnostic.Path{}, "MP3 reader requires a prepared inspection", nil))
			}
			properties, err := inspected.properties()
			if err != nil {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, err
			}
			output, err := stream.NewDescriptor(input.ID(), format.Chunks().Descriptor(), timing.MustBase(1, int64(inspected.signal.Rate)), properties)
			if err != nil {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, err
			}
			metadataValue, err := metadata.Merge(metadata.AssetScope, input.Metadata(), inspected.metadata)
			if err != nil {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, err
			}
			output = output.WithMetadata(metadataValue)
			return plugin.Compiled[readerPlan, stream.Descriptor]{
				Plan:      readerPlan{shape: shape.Clone(), inspection: inspected},
				Outputs:   flow.NewDescriptors(flow.Describe("chunks", output)),
				Effects:   []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "mp3.demux"}},
				Resources: resource.Request{},
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan readerPlan) (flow.Operator, error) {
			if !plan.inspection.valid() {
				return nil, errors.New("MP3 reader plan is invalid")
			}
			return newReader(plan), nil
		},
	}
	return plugin.NewComponent[readerID](plugin.Descriptor{DisplayName: "MP3 reader"}, configurationSchema(), plugin.WithSpec(spec), plugin.WithProcessor("bytes", access.Bytes(), "chunks", format.Chunks()), format.Read(MP3(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)), format.WithProbe(probeMP3), format.WithInspect(inspectMP3)))
}

func parserComponent() plugin.Component {
	shape := parserShape()
	spec := plugin.Spec[configuration, parserPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[parserPlan, stream.Descriptor], error) {
			input, ok := inputs.One("chunks")
			if !ok {
				return plugin.Compiled[parserPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("chunks", plugin.ConditionNeed[stream.Descriptor]("mp3.chunks"))}}, nil
			}
			signal, err := sample.SignalOf(input.Properties())
			if err != nil {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, err
			}
			if input.TimeBase() != timing.MustBase(1, int64(signal.Rate)) {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem("mp3.time-base", diagnostic.ErrorSeverity, diagnostic.Path{}, "MP3 parser requires a sample time base", nil))
			}
			value, hasFraming := framingOf(input.Properties())
			tag, tagged := codec.TagOf(input.Properties())
			if !tagged || tag != CodecTag() {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem("mp3.codec-tag", diagnostic.ErrorSeverity, diagnostic.Path{}, "MP3 parser requires a codec tag", nil))
			}
			maxFrame := frame.MaximumTableFrameSize()
			if signal.Layout.Count() != 1 && signal.Layout.Count() != 2 {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, ErrUnsupported
			}
			if hasFraming && (!value.valid() || value.rate != signal.Rate || value.channels != signal.Layout.Count()) {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem("mp3.framing", diagnostic.ErrorSeverity, diagnostic.Path{}, "MP3 framing properties disagree with the stream signal", nil))
			}
			if hasFraming {
				maxFrame = value.maxFrameBytes
			}
			if maxFrame < 4 {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, ErrUnsupported
			}
			if parserWorkspace(maxFrame) == math.MaxInt {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, ErrUnsupported
			}
			outputProperties := stream.WithoutDuration(withoutFraming(input.Properties()))
			output, err := stream.NewDescriptor(input.ID(), codec.Packets().Descriptor(), input.TimeBase(), outputProperties)
			if err != nil {
				return plugin.Compiled[parserPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[parserPlan, stream.Descriptor]{
				Plan:      parserPlan{shape: shape.Clone(), framing: value, hasFraming: hasFraming, signal: signal, maxFrame: maxFrame},
				Outputs:   flow.NewDescriptors(flow.Describe("packets", output.WithMetadata(input.Metadata()))),
				Effects:   []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.UnknownLoss, Detail: "mp3.parser"}},
				Resources: resource.Request{Memory: resource.Bytes(maxFrame)},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan parserPlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, errors.New("MP3 parser requires a payload buffer grant")
			}
			if plan.maxFrame < 4 || !plan.signal.Valid() {
				return nil, errors.New("MP3 parser plan is invalid")
			}
			return newParser(plan, ctx.Buffers()), nil
		},
	}
	return plugin.NewComponent[parserID](plugin.Descriptor{DisplayName: "MP3 parser"}, configurationSchema(), plugin.WithSpec(spec), plugin.WithProcessor("chunks", format.Chunks(), "packets", codec.Packets()))
}
