package solve

import (
	"strings"
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func lossyMetadataNodes() []plan.Node {
	return []plan.Node{
		{ID: "reader", Component: "fixture.reader", Effects: []plugin.Effect{
			{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "fixture.read"},
		}},
		{ID: "writer", Component: "fixture.writer", Effects: []plugin.Effect{
			{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "fixture.write"},
			{Kind: plugin.MetadataEffect, Loss: plugin.Lossy, Detail: "fixture.unrepresentable", Item: "godec/tag/composer"},
		}},
	}
}

// An encoding answers the same way whatever the job asked for: what a carrier
// can hold is a fact about the carrier. What the policy decides is whether
// that answer is fatal, and it decides it here rather than in each encoding.
func TestStrictMetadataRefusesWhatPreserveAccepts(t *testing.T) {
	preserve, _ := job.PolicyFor(job.Fast)
	if err := strictMetadata(preserve, lossyMetadataNodes()); err != nil {
		t.Fatalf("the default policy refused a partly writable conversion: %v", err)
	}

	strict := preserve
	strict.Metadata = job.StrictMetadata
	err := strictMetadata(strict, lossyMetadataNodes())
	if err == nil {
		t.Fatal("strict accepted a conversion that loses a key")
	}
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "solve.metadata-loss" {
		t.Fatalf("strict diagnostics = %#v", items)
	}
	// The report names the key and the node, because "something was lost" is
	// not an answer anyone can act on.
	if items[0].Detail["key"] != "godec/tag/composer" || items[0].Detail["node"] != "writer" {
		t.Fatalf("strict detail = %#v", items[0].Detail)
	}
	if !strings.Contains(items[0].Path.String(), "fixture.writer") {
		t.Fatalf("strict path = %s", items[0].Path.String())
	}
}

// Only a metadata effect that lost something is fatal. A conversion that
// reports metadata work without losing any of it still plans under strict.
func TestStrictMetadataIgnoresEffectsThatLostNothing(t *testing.T) {
	strict, _ := job.PolicyFor(job.Fast)
	strict.Metadata = job.StrictMetadata
	nodes := []plan.Node{{ID: "writer", Component: "fixture.writer", Effects: []plugin.Effect{
		{Kind: plugin.MetadataEffect, Loss: plugin.NoLoss, Detail: "fixture.rewritten"},
		{Kind: plugin.ContentEffect, Loss: plugin.Lossy, Detail: "fixture.gain"},
	}}}
	if err := strictMetadata(strict, nodes); err != nil {
		t.Fatalf("strict refused a conversion that lost no metadata: %v", err)
	}
}
