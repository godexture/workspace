package acme

import (
	"errors"
	"unicode/utf8"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/plugin"
)

type (
	labelCarrierID struct{}
	labelKeyID     struct{}
)

var label = key.Define[labelKeyID, string]()

func LabelCarrier() carrier.ID { return carrier.Define[labelCarrierID]() }
func Label() key.Key[string]   { return label }

func encodingComponent() plugin.Component {
	return plugin.NewComponent[encodingID](plugin.Descriptor{DisplayName: "ACME label encoding"}, configurationSchema(),
		metadata.WithEncoding(parseLabel, marshalLabel))
}

func parseLabel(ctx metadata.ParseContext) (metadata.Document, error) {
	value := ctx.Payload().AppendTo(nil)
	if len(value) == 0 || len(value) > maxLabelBytes || !utf8.Valid(value) {
		return metadata.Document{}, ErrMalformed
	}
	builder := metadata.NewBuilder(ctx.Scope())
	builder.AddBlock(metadata.NewRawBlock(ctx.Block(), ctx.Carrier(), ctx.Encoding(), ctx.Payload()))
	metadata.Add(builder, Label(), string(value), metadata.Origin{
		Encoding: ctx.Encoding(), Carrier: ctx.Carrier(), Block: ctx.Block(), Native: "label",
	})
	return builder.Build()
}

// marshalLabel writes the one label this carrier has room for. A document may
// hold several, because multiplicity is a fact about a document rather than
// about any one carrier; folding them is this encoding's job, and saying what
// the fold cost is the other half of that job.
func marshalLabel(ctx metadata.MarshalContext) (metadata.Blob, []metadata.Loss, error) {
	values := metadata.Values(ctx.Document(), Label())
	if len(values) == 0 || values[0] == "" || len(values[0]) > maxLabelBytes || !utf8.ValidString(values[0]) {
		return metadata.Blob{}, nil, errors.Join(ErrMalformed, errors.New("ACME metadata requires a valid label"))
	}
	var lost []metadata.Loss
	for range values[1:] {
		lost = append(lost, metadata.Loss{
			Key:    Label().ID(),
			Kind:   metadata.Folded,
			Native: "label",
			Detail: "acme.single-label",
		})
	}
	return metadata.NewBlob("text/plain; charset=utf-8", []byte(values[0])), lost, nil
}
