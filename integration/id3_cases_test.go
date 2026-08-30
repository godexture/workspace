package integration_test

import (
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/id3"
	"github.com/godexture/godec/testkit"
)

type id3V1CarrierID struct{}

func runID3V1Cases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	slot := carrier.Define[id3V1CarrierID]()
	block := metadata.BlockID("id3v1/tag")
	payload := id3V1Fixture()
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewSourceBlock(block, slot, id3.V1EncodingIdentity(), payload))
	origin := func(native string) metadata.Origin {
		return metadata.Origin{Carrier: slot, Encoding: id3.V1EncodingIdentity(), Block: block, Native: native}
	}
	metadata.Add(builder, tag.Title(), "Song", origin("title"))
	metadata.Add(builder, tag.Artist(), "Artist", origin("artist"))
	metadata.Add(builder, tag.TrackNumber(), int64(7), origin("track"))
	metadata.Add(builder, tag.Genre(), "Rock", origin("genre"))
	want, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	testkit.Metadata(t,
		testkit.TrackMetadata(testkit.MetadataIn(set, id3.V1EncodingIdentity()), coverage),
		testkit.MetadataCase{
			Name:  "v1-source-roundtrip",
			Input: testkit.MetadataInput(slot, block, metadata.StreamScope, payload),
			Want:  testkit.WantMetadata(want, payload),
		},
	)
}

func id3V1Fixture() metadata.Blob {
	value := make([]byte, 128)
	copy(value, "TAG")
	copy(value[3:33], "Song")
	copy(value[33:63], "Artist")
	value[125] = 0
	value[126] = 7
	value[127] = 17
	return metadata.NewBlob("application/x-id3v1", value)
}
