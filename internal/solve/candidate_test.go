package solve

import (
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

func TestValidateBridgeResultRejectsMetadataStateChange(t *testing.T) {
	document, err := metadata.NewBuilder(metadata.StreamScope).Build()
	if err != nil {
		t.Fatal(err)
	}
	input := solveDescriptor(solveSchemaA, 44_100).WithMetadata(metadata.MustAvailable(document))
	bridge := solveBridge[solveBridgeAAID](solveSchemaA, solveSchemaA, structural("state"), func(input stream.Descriptor, _ solveConfig) stream.Descriptor {
		return input.WithMetadata(metadata.Absent())
	}, nil, 0, plugin.Contract{}, nil, nil)
	resolved, err := bridge.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(bridge, plugin.CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("in", input)))
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := job.PolicyFor(job.Fast)
	_, err = validateBridgeResult(bridge, compiled, bridge.Ports().Outputs[0], input, policy, solvePlatform())
	if got := rejectionCode(err); got != "metadata-state" {
		t.Fatalf("metadata state rejection = %q (%v), want metadata-state", got, err)
	}
}
