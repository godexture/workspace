package evidence

import (
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
)

type metadataEvidenceCarrierID struct{}
type metadataEvidenceKeyID struct{}

func TestMetadataLossUsesFixedDetailSchema(t *testing.T) {
	detail := MetadataLoss(loss.Report{
		Carrier: carrier.Define[metadataEvidenceCarrierID](), Encoding: "fixture.encoding", Block: "fixture/block",
		Loss: loss.Loss{Key: key.Define[metadataEvidenceKeyID, string]().ID(), Kind: loss.Dropped, Detail: "fixture.reason"},
	})
	for _, field := range []string{
		"block", "carrier", "encoding", "key", "kind", "mapping", "native", "reason",
		"sourceBlock", "sourceCarrier", "sourceEncoding", "sourceNative", "target",
	} {
		if _, ok := detail[field]; !ok {
			t.Fatalf("metadata loss detail omitted fixed field %q: %#v", field, detail)
		}
	}
	if detail["mapping"] != "none" {
		t.Fatalf("non-converted mapping = %q, want none", detail["mapping"])
	}
	for _, field := range []string{"sourceBlock", "sourceCarrier", "sourceEncoding", "sourceNative", "target", "native"} {
		if detail[field] != "" {
			t.Fatalf("zero metadata loss field %q = %q", field, detail[field])
		}
	}
}
