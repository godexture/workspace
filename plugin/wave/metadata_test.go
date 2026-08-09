package wave

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

func TestInspectPreservesRIFFInfoAndUnknownChunkPlacement(t *testing.T) {
	beforeFormat := waveTestChunk(t, "JUNK", []byte{1, 2, 3}, 0x91)
	formatChunk := waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0)
	unknownField := infoTestChunk(t, "XTRA", []byte{4, 5, 6}, 0xa2)
	infoChunk := infoTestList(t, infoTestChunk(t, "INAM", []byte("Song\x00"), 0), unknownField)
	dataChunk := waveTestChunk(t, tagDATA, []byte{7, 8}, 0)
	afterData := waveTestChunk(t, "TAIL", []byte{9, 10, 11}, 0xb3)
	value := waveTestRIFF(t, beforeFormat, formatChunk, infoChunk, dataChunk, afterData)

	inspected, err := inspectHeaderWithMetadata(t.Context(), memoryRandom(value), infoTestResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	if title, ok := metadata.First(inspected.metadata, tag.Title()); !ok || title != "Song" {
		t.Fatalf("WAVE title = %q/%v", title, ok)
	}
	blocks := inspected.metadata.Blocks()
	if len(blocks) != 4 {
		t.Fatalf("WAVE raw blocks = %#v", blocks)
	}
	assertWaveChunkBlock(t, blocks[0], chunkBeforeFormat, chunkRaw, beforeFormat)
	assertWaveChunkBlock(t, blocks[1], chunkBeforeData, chunkInfo, infoChunk)
	if !bytes.Equal(blocks[2].Payload().AppendTo(nil), unknownField) {
		t.Fatalf("unknown INFO field = %x, want %x", blocks[2].Payload().AppendTo(nil), unknownField)
	}
	assertWaveChunkBlock(t, blocks[3], chunkAfterData, chunkRaw, afterData)

	provider := metadata.NewBuilder(metadata.StreamScope)
	metadata.Add(provider, tag.Comment(), "provider", metadata.Origin{})
	providerDocument, err := provider.Build()
	if err != nil {
		t.Fatal(err)
	}
	compileContext, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(WAVE(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	component := demuxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor("wave", access.Bytes().Identity(), access.CarrierTimeBase(), property.New()).WithMetadata(providerDocument)
	compiled, err := plugin.Compile(component, compileContext, resolved, flow.NewDescriptors(flow.Describe("bytes", input)))
	if err != nil {
		t.Fatal(err)
	}
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok {
		t.Fatal("WAVE outputs have the wrong descriptor type")
	}
	output, ok := outputs.One("chunks")
	if !ok {
		t.Fatal("WAVE demux output is absent")
	}
	entries := output.Metadata().Entries()
	if len(entries) != 2 || entries[0].Key() != tag.Comment().ID() || entries[1].Key() != tag.Title().ID() {
		t.Fatalf("merged WAVE metadata order = %#v", entries)
	}
}

func TestInspectReportsMissingRIFFInfoBinding(t *testing.T) {
	value := waveTestRIFF(t,
		waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0),
		infoTestList(t, infoTestChunk(t, "INAM", []byte("Song\x00"), 0)),
		waveTestChunk(t, tagDATA, []byte{1, 2}, 0),
	)
	_, err := inspectHeaderWithMetadata(t.Context(), memoryRandom(value), metadata.Resolver{})
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == "metadata.binding-missing" && item.Detail["carrier"] == RIFFInfo().String() {
			return
		}
	}
	t.Fatalf("missing RIFF INFO binding diagnostic = %v", err)
}

func assertWaveChunkBlock(t testing.TB, block metadata.RawBlock, anchor chunkAnchor, kind chunkKind, payload []byte) {
	t.Helper()
	parsed, ok := parseChunkBlockID(block.ID())
	if !ok || parsed.anchor != anchor || parsed.kind != kind || !bytes.Equal(block.Payload().AppendTo(nil), payload) {
		t.Fatalf("WAVE chunk block = %#v, parsed %#v/%v", block, parsed, ok)
	}
	switch kind {
	case chunkInfo:
		if block.Carrier() != RIFFInfo() || block.Encoding() != InfoEncodingIdentity() {
			t.Fatalf("RIFF INFO provenance = %#v", block)
		}
	case chunkRaw:
		if block.Carrier() != rawChunkCarrier() || !block.Encoding().IsZero() {
			t.Fatalf("raw WAVE chunk provenance = %#v", block)
		}
	}
}

func waveTestChunk(t testing.TB, identity string, payload []byte, padding byte) []byte {
	t.Helper()
	value, err := marshalInfoChunk(identity, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload)&1 != 0 {
		value[len(value)-1] = padding
	}
	return value
}

func waveTestRIFF(t testing.TB, chunks ...[]byte) []byte {
	t.Helper()
	body := []byte(tagWAVE)
	for _, chunk := range chunks {
		body = append(body, chunk...)
	}
	if uint64(len(body)) > uint64(^uint32(0)) {
		t.Fatal("WAVE test body exceeds RIFF")
	}
	value := make([]byte, 8+len(body))
	copy(value[0:4], tagRIFF)
	binary.LittleEndian.PutUint32(value[4:8], uint32(len(body)))
	copy(value[8:], body)
	return value
}
