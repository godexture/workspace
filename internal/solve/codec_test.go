package solve

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type solveTaggedSourceID struct{}
type solveMatchingCodecID struct{}
type solveMatchingParserID struct{}
type solveOtherParserID struct{}
type solveUnboundBridgeID struct{}

func solveTaggedSource(t testing.TB, tag format.Tag) plugin.Component {
	t.Helper()
	properties, err := codec.WithTag(property.New(), tag)
	if err != nil {
		t.Fatal(err)
	}
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", solveSchemaA)})
	return solveComponent[solveTaggedSourceID](shape, func(solveConfig, flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		output := stream.MustDescriptor("stream", solveSchemaA.Descriptor(), timing.MustBase(1, 48_000), properties)
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", output))}
	}, nil, 0, plugin.Contract{}, nil, nil)
}

func codecDeclarations(values ...plugin.Declaration) []plugin.Declaration {
	return append(codec.Declarations(), values...)
}

func TestTaggedInputExcludesOnlyOtherCodecTargets(t *testing.T) {
	matchingTag := format.NewTag("fixture", "matching")
	otherTag := format.NewTag("fixture", "other")
	source := solveTaggedSource(t, matchingTag)
	sink := solveSink(solveSchemaB, false, nil)
	var matchingCodecCompiles, matchingParserCompiles, otherParserCompiles, unboundCompiles atomic.Int32
	matchingCodec := solveBridge[solveMatchingCodecID](solveSchemaA, solveSchemaB, structural("matching-codec"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, &matchingCodecCompiles)
	matchingParser := solveBridge[solveMatchingParserID](solveSchemaA, solveSchemaB, structural("matching-parser"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, &matchingParserCompiles)
	otherParser := solveBridge[solveOtherParserID](solveSchemaA, solveSchemaB, structural("other-parser"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, &otherParserCompiles)
	unbound := solveBridge[solveUnboundBridgeID](solveSchemaA, solveSchemaB, structural("unbound"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, &unboundCompiles)
	declarations := codecDeclarations(
		codec.BindDecoder(matchingTag, codec.New(matchingCodec.Identity())),
		codec.BindParser(matchingTag, codec.NewParser(matchingParser.Identity())),
		codec.BindDecoder(otherTag, codec.New(matchingCodec.Identity())),
		codec.BindParser(otherTag, codec.NewParser(otherParser.Identity())),
	)
	index := solveIndexWithDeclarations(t, declarations, source, sink, matchingCodec, matchingParser, otherParser, unbound)

	program, err := Resolve(context.Background(), index, solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	if matchingCodecCompiles.Load() == 0 || matchingParserCompiles.Load() == 0 || unboundCompiles.Load() == 0 {
		t.Fatalf("matching/unbound compiles = %d/%d/%d", matchingCodecCompiles.Load(), matchingParserCompiles.Load(), unboundCompiles.Load())
	}
	if otherParserCompiles.Load() != 0 {
		t.Fatalf("other-tag parser compiled %d times", otherParserCompiles.Load())
	}
	for _, node := range program.Plan().Nodes() {
		if node.Component == otherParser.Identity().String() {
			t.Fatal("other-tag parser entered the selected Plan")
		}
	}
}

func TestCodecTagMismatchAppearsInPlanningDiagnostic(t *testing.T) {
	requestedTag := format.NewTag("fixture", "requested")
	otherTag := format.NewTag("fixture", "other")
	source := solveTaggedSource(t, requestedTag)
	sink := solveSink(solveSchemaB, false, nil)
	var compiles atomic.Int32
	other := solveBridge[solveOtherParserID](solveSchemaA, solveSchemaB, structural("other"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, &compiles)
	index := solveIndexWithDeclarations(t, codecDeclarations(codec.BindDecoder(otherTag, codec.New(other.Identity()))), source, sink, other)

	_, err := Resolve(context.Background(), index, solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if compiles.Load() != 0 {
		t.Fatalf("other-tag component compiled %d times", compiles.Load())
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "solve.unsupported" && item.Detail["codecTag"] == requestedTag.String() && strings.Contains(item.Detail["rejections"], "codec-tag=1") {
			return
		}
	}
	t.Fatalf("codec-tag planning diagnostic = %v", err)
}
