package integration_test

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/testkit"
)

func runRIFFInfoCases(t *testing.T, set plugin.Set, coverage *testkit.Coverage) {
	t.Helper()
	title := riffInfoField("INAM", []byte("Song\x00"), 0x7f)
	artistFirst := riffInfoField("IART", []byte("First\x00"), 0)
	artistSecond := riffInfoField("IART", []byte("Second\x00"), 0xa5)
	unknown := riffInfoField("XTRA", []byte{1, 2, 3}, 0xcc)
	value := riffInfoList(title, artistFirst, artistSecond, unknown)
	payload := metadata.NewBlob("application/x-riff-info", value)
	block := metadata.BlockID("list-0")
	origin := func(native string) metadata.Origin {
		return metadata.Origin{Encoding: wave.InfoEncodingIdentity(), Carrier: wave.RIFFInfo(), Block: block, Native: native}
	}
	builder := metadata.NewBuilder(metadata.StreamScope)
	builder.AddBlock(metadata.NewRawBlock(block, wave.RIFFInfo(), wave.InfoEncodingIdentity(), payload))
	unknownOffset := 4 + len(title) + len(artistFirst) + len(artistSecond)
	builder.AddBlock(metadata.NewRawBlock(
		metadata.BlockID(fmt.Sprintf("%s/field/%08d", block, unknownOffset)),
		wave.RIFFInfo(), wave.InfoEncodingIdentity(), metadata.NewBlob("application/octet-stream", unknown),
	))
	metadata.Add(builder, tag.Title(), "Song", origin("INAM"))
	metadata.Add(builder, tag.Artist(), "First", origin("IART"))
	metadata.Add(builder, tag.Artist(), "Second", origin("IART"))
	want, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}

	testkit.Metadata(t,
		testkit.TrackMetadata(testkit.MetadataIn(set, wave.InfoEncodingIdentity()), coverage),
		testkit.MetadataCase{
			Name:  "duplicates-order-and-unknown-raw",
			Input: testkit.MetadataInput(wave.RIFFInfo(), block, metadata.StreamScope, payload),
			Want:  testkit.WantMetadata(want, payload),
		},
		testkit.MetadataCase{
			Name:  "malformed",
			Input: testkit.MetadataInput(wave.RIFFInfo(), "broken", metadata.StreamScope, metadata.NewBlob("", nil)),
			Want:  testkit.MetadataFails("metadata.parse"),
		},
	)
}

func riffInfoField(identity string, payload []byte, padding byte) []byte {
	result := make([]byte, 8+len(payload)+len(payload)&1)
	copy(result[0:4], identity)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(payload)))
	copy(result[8:], payload)
	if len(payload)&1 != 0 {
		result[len(result)-1] = padding
	}
	return result
}

func riffInfoList(fields ...[]byte) []byte {
	payload := []byte("INFO")
	for _, field := range fields {
		payload = append(payload, field...)
	}
	result := make([]byte, 8+len(payload)+len(payload)&1)
	copy(result[0:4], "LIST")
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(payload)))
	copy(result[8:], payload)
	return result
}
