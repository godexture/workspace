package metadata

import (
	"testing"

	"github.com/godexture/godec/media/carrier"
)

type lossCarrierID struct{}

// A loss says what became of a value, and a conversion is the only kind that
// also says what the conversion cost. Anything else claiming a cost is
// describing something that did not happen.
func TestOnlyAConversionCarriesWhatItCost(t *testing.T) {
	for _, test := range []struct {
		name  string
		value Loss
		valid bool
	}{
		{name: "dropped", value: Loss{Key: title.ID(), Kind: Dropped}, valid: true},
		{name: "folded", value: Loss{Key: title.ID(), Kind: Folded, Native: "TIT2"}, valid: true},
		{name: "converted", value: Loss{Key: title.ID(), Kind: Converted, Mapping: Approximate}, valid: true},
		{name: "converted-without-cost", value: Loss{Key: title.ID(), Kind: Converted}},
		{name: "dropped-with-cost", value: Loss{Key: title.ID(), Kind: Dropped, Mapping: Lossless}},
		{name: "no-key", value: Loss{Kind: Dropped}},
		{name: "no-kind", value: Loss{Key: title.ID()}},
	} {
		if got := test.value.Valid(); got != test.valid {
			t.Errorf("%s valid = %v, want %v", test.name, got, test.valid)
		}
	}
}

// An encoding that reports a loss it cannot describe is a bug in the encoding,
// so the report is refused rather than passed on as evidence.
func TestMarshalRefusesAnUndescribedLoss(t *testing.T) {
	component := encodingTraitComponent(
		func(ctx ParseContext) (Document, error) { return NewBuilder(ctx.Scope()).Build() },
		func(MarshalContext) (Blob, []Loss, error) {
			return NewBlob("text/plain", []byte("x")), []Loss{{Kind: Dropped}}, nil
		},
	)
	encoding, ok := EncodingOf(component)
	if !ok || !encoding.Valid() {
		t.Fatal("fixture encoding is invalid")
	}
	document, err := NewBuilder(AssetScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = encoding.Marshal(MarshalContext{
		carrier:  carrier.Define[lossCarrierID](),
		block:    "block",
		encoding: component.Identity(),
		document: document,
	})
	if err == nil {
		t.Fatal("an undescribed loss was accepted")
	}
}
