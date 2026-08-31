package integration_test

import (
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/mp4"
	"github.com/godexture/godec/testkit"
)

type ilstConformanceCarrierID struct{}

func runIlstCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	slot := carrier.Define[ilstConformanceCarrierID]()
	block := metadata.BlockID("mp4/ilst")
	name := [4]byte{0xa9, 'n', 'a', 'm'}
	data := binary.BigEndian.AppendUint32(nil, 1)
	data = binary.BigEndian.AppendUint32(data, 0)
	data = append(data, "Song"...)
	title := ilstConformanceAtom(name, ilstConformanceAtom([4]byte{'d', 'a', 't', 'a'}, data))
	unknown := ilstConformanceAtom([4]byte{'-', '-', '-', '-'}, []byte{0xff, 0, 1})
	payload := metadata.NewBlob("application/x-itunes-ilst", append(title, unknown...))
	builder := metadata.NewBuilder(metadata.AssetScope)
	builder.AddBlock(metadata.NewSourceBlock(block, slot, mp4.IlstEncodingIdentity(), payload))
	builder.AddBlock(metadata.NewRawBlock(block+"/item/00000001", slot, mp4.IlstEncodingIdentity(), metadata.NewBlob("application/x-itunes-ilst-item", unknown)))
	metadata.Add(builder, tag.Title(), "Song", metadata.Origin{Carrier: slot, Encoding: mp4.IlstEncodingIdentity(), Block: block, Native: string(name[:])})
	want, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	testkit.Metadata(t,
		testkit.TrackMetadata(testkit.MetadataIn(set, mp4.IlstEncodingIdentity()), coverage),
		testkit.MetadataCase{
			Name:  "source-roundtrip-with-opaque-item",
			Input: testkit.MetadataInput(slot, block, metadata.AssetScope, payload),
			Want:  testkit.WantMetadata(want, payload),
		},
	)
}

func ilstConformanceAtom(typeID [4]byte, payload []byte) []byte {
	result := binary.BigEndian.AppendUint32(nil, uint32(8+len(payload)))
	result = append(result, typeID[:]...)
	return append(result, payload...)
}
