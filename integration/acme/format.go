package acme

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type (
	formatID         struct{}
	readerInspection struct {
		offset   int64
		metadata metadata.Document
	}
	readerPlan struct {
		shape  flow.Shape
		offset int64
	}
)

func Container() mediaformat.Format {
	value, err := mediaformat.Define[formatID]([]carrier.ID{LabelCarrier()}, mediaformat.WithExtensions("acme"))
	if err != nil {
		panic(err)
	}
	return value
}

func CodecTag() mediaformat.Tag { return mediaformat.NewTag("acme", "increment") }

func readerComponent() plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("bytes", access.Bytes())},
		[]flow.Port{flow.Out("packets", codec.Packets())},
	)
	spec := plugin.Spec[configuration, readerPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[configuration](shape),
		Compile: func(ctx plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[readerPlan, stream.Descriptor], error) {
			input, ok := inputs.One("bytes")
			if !ok {
				return plugin.Compiled[readerPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("acme.input")),
				}}, nil
			}
			inspected, ok := mediaformat.InspectionOf[readerInspection](ctx, Container())
			if !ok || inspected.offset <= 5 || !inspected.metadata.Scope().Valid() {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, errors.New("ACME reader requires prepared inspection")
			}
			properties, err := codec.WithTag(property.New(), CodecTag())
			if err != nil {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, err
			}
			output, err := stream.NewDescriptor(input.ID(), codec.Packets().Identity(), timing.MustBase(1, 1), properties)
			if err != nil {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, err
			}
			document, err := metadata.NewBuilder(metadata.StreamScope).Append(input.Metadata()).Append(inspected.metadata).Build()
			if err != nil {
				return plugin.Compiled[readerPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[readerPlan, stream.Descriptor]{
				Plan:    readerPlan{shape: shape.Clone(), offset: inspected.offset},
				Outputs: flow.NewDescriptors(flow.Describe("packets", output.WithMetadata(document))),
				Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "acme-demux"}},
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan readerPlan) (flow.Operator, error) {
			if plan.offset <= 5 {
				return nil, ErrMalformed
			}
			return &readerOperator{shape: plan.shape.Clone(), offset: plan.offset}, nil
		},
	}
	return plugin.NewComponent[readerID](plugin.Descriptor{DisplayName: "ACME reader"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("bytes", access.Bytes(), "packets", codec.Packets()),
		mediaformat.Read(Container(), access.NewRequirements(access.AnyOf(access.RandomRead)), mediaformat.WithProbe(probe), mediaformat.WithInspect(inspect)),
	)
}

func probe(ctx mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
	var signature [4]byte
	for _, view := range ctx.Views() {
		if _, err := view.ReadAt(ctx.Context(), signature[:], 0); err == nil {
			if !bytes.Equal(signature[:], []byte("ACM1")) {
				return mediaformat.Mismatch(), nil
			}
			evidence, err := mediaformat.NewEvidence("ACME magic")
			if err != nil {
				return mediaformat.ProbeResult{}, err
			}
			return mediaformat.Match(evidence), nil
		}
	}
	request, err := access.NewRangeRequest(0, 4)
	if err != nil {
		return mediaformat.ProbeResult{}, err
	}
	return mediaformat.Need(request), nil
}

func inspect(ctx mediaformat.InspectContext) (mediaformat.Inspection, error) {
	reader, ok := access.RandomOf(ctx.Opening())
	if !ok {
		return mediaformat.Inspection{}, ErrMalformed
	}
	header := make([]byte, 5)
	if err := readFullAt(ctx.Context(), reader, header, 0); err != nil || !bytes.Equal(header[:4], []byte("ACM1")) {
		return mediaformat.Inspection{}, errors.Join(ErrMalformed, err)
	}
	length := int(header[4])
	if length == 0 || length > maxLabelBytes {
		return mediaformat.Inspection{}, ErrMalformed
	}
	labelBytes := make([]byte, length)
	if err := readFullAt(ctx.Context(), reader, labelBytes, 5); err != nil {
		return mediaformat.Inspection{}, errors.Join(ErrMalformed, err)
	}
	var payload [1]byte
	if err := readFullAt(ctx.Context(), reader, payload[:], int64(5+length)); err != nil {
		return mediaformat.Inspection{}, errors.Join(ErrMalformed, err)
	}
	resolver, ok := metadata.ResolverOf(ctx.Prepared())
	if !ok {
		return mediaformat.Inspection{}, errors.New("ACME reader requires metadata resolver")
	}
	document, err := resolver.Parse(ctx.Context(), LabelCarrier(), "acme/label", metadata.StreamScope, metadata.NewBlob("text/plain; charset=utf-8", labelBytes))
	if err != nil {
		return mediaformat.Inspection{}, err
	}
	return mediaformat.NewInspection(Container(), readerInspection{offset: int64(5 + length), metadata: document}), nil
}

func readFullAt(ctx context.Context, reader access.Random, destination []byte, offset int64) error {
	read := 0
	for read < len(destination) {
		count, err := reader.ReadAt(ctx, destination[read:], offset+int64(read))
		if count < 0 || count > len(destination)-read {
			return ErrMalformed
		}
		read += count
		if err != nil {
			if errors.Is(err, io.EOF) && read == len(destination) {
				return nil
			}
			return err
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

type readerOperator struct {
	shape    flow.Shape
	offset   int64
	absolute int64
	sequence uint64
	emitted  bool
}

func (o *readerOperator) Ports() flow.Shape { return o.shape.Clone() }
func (*readerOperator) Close() error        { return nil }

func (o *readerOperator) Process(ctx context.Context, input flow.Input[buffer.Handle], output flow.Emitter[packet.Packet]) error {
	if !input.Valid() {
		return ErrMalformed
	}
	defer input.Drop()
	data := input.Value().Bytes()
	start := o.absolute
	o.absolute += int64(len(data))
	local := 0
	if start < o.offset {
		local = int(o.offset - start)
		if local >= len(data) {
			return nil
		}
	}
	payload, err := input.Value().Range(local, len(data)-local)
	if err != nil {
		return err
	}
	value := packet.NewPacket(o.sequence, timing.UnknownPTS(), timing.UnknownDTS(), timing.UnknownDuration(), payload)
	item := flow.NewInput(value, codec.Packets())
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	o.sequence++
	o.emitted = true
	return nil
}

func (o *readerOperator) Flush(context.Context, flow.Emitter[packet.Packet]) error {
	if o.absolute < o.offset || !o.emitted {
		return ErrMalformed
	}
	return nil
}
