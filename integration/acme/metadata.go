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

func marshalLabel(ctx metadata.MarshalContext) (metadata.Blob, error) {
	values := metadata.Values(ctx.Document(), Label())
	if len(values) != 1 || values[0] == "" || len(values[0]) > maxLabelBytes || !utf8.ValidString(values[0]) {
		return metadata.Blob{}, errors.Join(ErrMalformed, errors.New("ACME metadata requires exactly one valid label"))
	}
	return metadata.NewBlob("text/plain; charset=utf-8", []byte(values[0])), nil
}
