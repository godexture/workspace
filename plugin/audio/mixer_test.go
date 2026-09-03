package audio

import (
	"errors"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type mixerMetadataCarrierID struct{}

var mixerMetadataCarrier = carrier.Define[mixerMetadataCarrierID]()

func mixerInput(t testing.TB, id stream.ID, attachment metadata.Attachment) stream.Descriptor {
	t.Helper()
	signal := sample.Signal{Rate: 48_000, Layout: sample.Mono(), ValidBits: 32}
	properties, err := processed(signal).Properties()
	if err != nil {
		t.Fatal(err)
	}
	value, err := stream.NewDescriptor(id, sample.Frames[float32]().Descriptor(), timing.MustBase(1, 48_000), properties)
	if err != nil {
		t.Fatal(err)
	}
	return value.WithMetadata(attachment)
}

func mixerDocument(t testing.TB, scope metadata.Scope, blockID metadata.BlockID, title string) metadata.Document {
	t.Helper()
	builder := metadata.NewBuilder(scope)
	builder.AddBlock(metadata.NewRawBlock(blockID, mixerMetadataCarrier, plugin.Identity{}, metadata.NewBlob("application/octet-stream", []byte(blockID))))
	metadata.Add(builder, tag.Title(), title, metadata.Origin{})
	document, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func compileMixerMetadata(t testing.TB, values ...metadata.Attachment) (stream.Descriptor, error) {
	t.Helper()
	inputs := make([]flow.PortDescriptor[stream.Descriptor], len(values))
	for index, value := range values {
		inputs[index] = flow.Describe("inputs", mixerInput(t, stream.ID("mixer-"+string(rune('a'+index))), value))
	}
	shape := newMixer().Ports()
	compiled, err := compileMixer(shape, mixerConfig{MaxSamples: defaultFilterSamples}, flow.NewDescriptors(inputs...))
	if err != nil {
		return stream.Descriptor{}, err
	}
	output, ok := compiled.Outputs.One("mixed")
	if !ok {
		t.Fatal("mixer metadata test produced no output descriptor")
	}
	return output, nil
}

func TestMixerMergesAvailableMetadataInInputOrder(t *testing.T) {
	left := mixerDocument(t, metadata.StreamScope, "left", "left")
	right := mixerDocument(t, metadata.StreamScope, "right", "right")
	output, err := compileMixerMetadata(t, metadata.MustAvailable(left), metadata.MustAvailable(right))
	if err != nil {
		t.Fatal(err)
	}
	document, err := output.Metadata().Semantic()
	if err != nil {
		t.Fatal(err)
	}
	if values := metadata.Values(document, tag.Title()); len(values) != 2 || values[0] != "left" || values[1] != "right" {
		t.Fatalf("mixer metadata values = %v", values)
	}
	blocks := document.Blocks()
	if len(blocks) != 2 || blocks[0].ID() != "left" || blocks[1].ID() != "right" {
		t.Fatalf("mixer metadata blocks = %#v", blocks)
	}
}

func TestMixerPreservesFirstNonAbsentMetadataScope(t *testing.T) {
	left := mixerDocument(t, metadata.AssetScope, "left-asset", "left")
	right := mixerDocument(t, metadata.AssetScope, "right-asset", "right")
	for _, inputs := range [][]metadata.Attachment{
		{metadata.MustAvailable(left)},
		{metadata.MustAvailable(left), metadata.Absent(), metadata.MustAvailable(right)},
	} {
		output, err := compileMixerMetadata(t, inputs...)
		if err != nil {
			t.Fatal(err)
		}
		if !output.Metadata().IsAvailable() || output.Metadata().Scope() != metadata.AssetScope {
			t.Fatalf("mixer asset metadata state = %s/%s", output.Metadata().State(), output.Metadata().Scope())
		}
	}
}

func TestMixerDeduplicatesOneSharedMetadataDocument(t *testing.T) {
	shared := mixerDocument(t, metadata.AssetScope, "shared", "shared")
	output, err := compileMixerMetadata(t, metadata.MustAvailable(shared), metadata.MustAvailable(shared))
	if err != nil {
		t.Fatal(err)
	}
	document, err := output.Metadata().Semantic()
	if err != nil {
		t.Fatal(err)
	}
	if values := metadata.Values(document, tag.Title()); len(values) != 1 || values[0] != "shared" {
		t.Fatalf("shared metadata values = %v", values)
	}
	if blocks := document.Blocks(); len(blocks) != 1 || blocks[0].ID() != "shared" {
		t.Fatalf("shared metadata blocks = %#v", blocks)
	}
}

func TestMixerUnavailableMetadataDominatesEveryAvailableState(t *testing.T) {
	values := []struct {
		name  string
		input []metadata.Attachment
	}{
		{name: "absent and unavailable", input: []metadata.Attachment{metadata.Absent(), metadata.MustUnavailable(metadata.StreamScope)}},
		{name: "available and unavailable", input: []metadata.Attachment{metadata.MustAvailable(mixerDocument(t, metadata.StreamScope, "left", "left")), metadata.MustUnavailable(metadata.StreamScope)}},
	}
	for _, test := range values {
		t.Run(test.name, func(t *testing.T) {
			output, err := compileMixerMetadata(t, test.input...)
			if err != nil {
				t.Fatal(err)
			}
			if !output.Metadata().IsUnavailable() {
				t.Fatalf("mixer metadata state = %s", output.Metadata().State())
			}
			if _, err := output.Metadata().Semantic(); !errors.Is(err, metadata.ErrMetadataUnavailable) {
				t.Fatalf("mixer unavailable semantic error = %v", err)
			}
		})
	}
}

func TestMixerRejectsMetadataScopeMismatchAndDuplicateBlocks(t *testing.T) {
	streamDocument := mixerDocument(t, metadata.StreamScope, "stream", "stream")
	assetDocument := mixerDocument(t, metadata.AssetScope, "asset", "asset")
	if _, err := compileMixerMetadata(t, metadata.MustAvailable(streamDocument), metadata.MustAvailable(assetDocument)); !errors.Is(err, metadata.ErrMetadataScopeMismatch) {
		t.Fatalf("mixer scope mismatch = %v", err)
	}
	duplicateFirst := mixerDocument(t, metadata.StreamScope, "same", "duplicate-first")
	duplicateSecond := mixerDocument(t, metadata.StreamScope, "same", "duplicate-second")
	if _, err := compileMixerMetadata(t, metadata.MustAvailable(duplicateFirst), metadata.MustAvailable(duplicateSecond)); err == nil {
		t.Fatal("mixer accepted duplicate metadata block")
	}
}
