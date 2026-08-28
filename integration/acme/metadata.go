package acme

import (
	"errors"
	"unicode/utf8"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
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
		metadata.WithEncoding(parseLabel, marshalLabel, Label().Erased()),
		metadata.WithMappings(metadata.Map(Label(), tag.Title(), loss.Lossless, 0, func(value string) (string, bool) { return value, true })))
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
func marshalLabel(ctx metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
	var label string
	found := false
	var lost []loss.Loss
	for _, entry := range ctx.Document().Entries() {
		if entry.Key() != Label().ID() {
			lost = append(lost, loss.Loss{
				Key:    entry.Key(),
				Kind:   loss.Dropped,
				Detail: "acme.label-unrepresentable",
				Source: entry.Origin().LossOrigin(),
			})
			continue
		}
		value, ok := entry.Value().(string)
		if !ok {
			return metadata.Blob{}, nil, errors.Join(ErrMalformed, errors.New("ACME metadata label has an invalid value"))
		}
		if !found {
			label = value
			found = true
			continue
		}
		lost = append(lost, loss.Loss{
			Key:    Label().ID(),
			Kind:   loss.Folded,
			Native: "label",
			Detail: "acme.single-label",
		})
	}
	if !found || label == "" || len(label) > maxLabelBytes || !utf8.ValidString(label) {
		return metadata.Blob{}, nil, errors.Join(ErrMalformed, errors.New("ACME metadata requires a valid label"))
	}
	return metadata.NewBlob("text/plain; charset=utf-8", []byte(label)), lost, nil
}
