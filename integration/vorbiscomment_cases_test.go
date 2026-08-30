package integration_test

import (
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/vorbiscomment"
	"github.com/godexture/godec/testkit"
)

type vorbisCommentCarrierID struct{}

func runVorbisCommentCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	slot := carrier.Define[vorbisCommentCarrierID]()
	block := metadata.BlockID("vorbis-comment/block")
	payload := vorbisCommentFixture()
	builder := metadata.NewBuilder(metadata.AssetScope)
	builder.AddBlock(metadata.NewSourceBlock(block, slot, vorbiscomment.EncodingIdentity(), payload))
	builder.AddBlock(metadata.NewRawBlock(block+"/vendor", slot, vorbiscomment.EncodingIdentity(), metadata.NewBlob("application/x-vorbis-comment-vendor", []byte("fixture"))))
	metadata.Add(builder, tag.Title(), "Song", metadata.Origin{Carrier: slot, Encoding: vorbiscomment.EncodingIdentity(), Block: block, Native: "TITLE"})
	metadata.Add(builder, tag.Artist(), "Artist", metadata.Origin{Carrier: slot, Encoding: vorbiscomment.EncodingIdentity(), Block: block, Native: "ARTIST"})
	want, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	testkit.Metadata(t,
		testkit.TrackMetadata(testkit.MetadataIn(set, vorbiscomment.EncodingIdentity()), coverage),
		testkit.MetadataCase{
			Name:  "source-roundtrip",
			Input: testkit.MetadataInput(slot, block, metadata.AssetScope, payload),
			Want:  testkit.WantMetadata(want, payload),
		},
	)
}

func vorbisCommentFixture() metadata.Blob {
	value := appendVorbisString(nil, "fixture")
	value = binary.LittleEndian.AppendUint32(value, 2)
	value = appendVorbisString(value, "TITLE=Song")
	value = appendVorbisString(value, "ARTIST=Artist")
	return metadata.NewBlob("application/x-vorbis-comment", value)
}

func appendVorbisString(destination []byte, value string) []byte {
	destination = binary.LittleEndian.AppendUint32(destination, uint32(len(value)))
	return append(destination, value...)
}
