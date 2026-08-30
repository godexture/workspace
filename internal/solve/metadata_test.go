package solve

import (
	"strings"
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/plan"
)

type metadataLossCarrierID struct{}
type metadataLossKeyID struct{}
type metadataLossTargetID struct{}

var (
	metadataLossCarrier = carrier.Define[metadataLossCarrierID]()
	metadataLossKey     = key.Define[metadataLossKeyID, string]().ID()
	metadataLossTarget  = key.Define[metadataLossTargetID, string]().ID()
)

func lossyMetadata() []plan.PredictedMetadataLoss {
	return []plan.PredictedMetadataLoss{{
		Output: 0, Node: "writer", Component: "fixture.writer", Port: "writes",
		Report: loss.Report{Carrier: metadataLossCarrier, Encoding: "fixture.encoding", Block: "fixture/block", Loss: loss.Loss{
			Key: metadataLossKey, Kind: loss.Dropped, Detail: "fixture.unrepresentable",
		}},
	}}
}

// An encoding answers the same way whatever the job asked for: what a carrier
// can hold is a fact about the carrier. What the policy decides is whether
// that answer is fatal, and it decides it here rather than in each encoding.
func TestStrictMetadataRefusesWhatPreserveAccepts(t *testing.T) {
	preserve, _ := job.PolicyFor(job.Fast)
	if err := strictMetadata(preserve, lossyMetadata()); err != nil {
		t.Fatalf("the default policy refused a partly writable conversion: %v", err)
	}

	strict := preserve
	strict.Metadata = job.StrictMetadata
	err := strictMetadata(strict, lossyMetadata())
	if err == nil {
		t.Fatal("strict accepted a conversion that loses a key")
	}
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "solve.metadata-loss" {
		t.Fatalf("strict diagnostics = %#v", items)
	}
	if items[0].Detail["key"] != metadataLossKey.String() || items[0].Detail["node"] != "writer" || items[0].Detail["block"] != "fixture/block" {
		t.Fatalf("strict detail = %#v", items[0].Detail)
	}
	if !strings.Contains(items[0].Path.String(), "fixture.writer") {
		t.Fatalf("strict path = %s", items[0].Path.String())
	}
}

func TestStrictMetadataIgnoresLosslessConversion(t *testing.T) {
	strict, _ := job.PolicyFor(job.Fast)
	strict.Metadata = job.StrictMetadata
	losses := lossyMetadata()
	losses[0].Report.Loss = loss.Loss{
		Key: metadataLossKey, Kind: loss.Converted, Target: metadataLossTarget,
		Mapping: loss.Lossless, Detail: "fixture.lossless-mapping",
	}
	if err := strictMetadata(strict, losses); err != nil {
		t.Fatalf("strict refused a declared lossless conversion: %v", err)
	}
	if warning := metadataWarning(losses); warning != "" {
		t.Fatalf("lossless conversion warning = %q", warning)
	}
}

func TestStrictMetadataTreatsSubstitutionAsLossy(t *testing.T) {
	preserve, _ := job.PolicyFor(job.Fast)
	losses := lossyMetadata()
	losses[0].Report.Loss = loss.Loss{Key: metadataLossKey, Kind: loss.Substituted, Detail: "fixture.substitution"}
	if err := strictMetadata(preserve, losses); err != nil {
		t.Fatalf("preserve refused a substitution: %v", err)
	}
	if warning := metadataWarning(losses); warning == "" {
		t.Fatal("substitution produced no metadata warning")
	}
	strict := preserve
	strict.Metadata = job.StrictMetadata
	if err := strictMetadata(strict, losses); err == nil {
		t.Fatal("strict accepted a substitution")
	}
}
