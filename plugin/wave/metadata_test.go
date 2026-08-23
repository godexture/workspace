package wave

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type cancelInfoEncodingID struct{}
type cancelInfoContextKey struct{}

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
	if len(blocks) != 0 {
		t.Fatalf("WAVE inspection retained opaque blocks = %#v", blocks)
	}
	if got := sourceRangeBytes(t, value, inspected.ranges.beforeFormat); !bytes.Equal(got, beforeFormat) {
		t.Fatalf("before-format range = %x, want %x", got, beforeFormat)
	}
	if got := sourceRangeBytes(t, value, inspected.ranges.beforeData); !bytes.Equal(got, infoChunk) {
		t.Fatalf("before-data range = %x, want %x", got, infoChunk)
	}
	if got := sourceRangeBytes(t, value, inspected.ranges.afterData); !bytes.Equal(got, afterData) {
		t.Fatalf("after-data range = %x, want %x", got, afterData)
	}
	infoRange := sourceRangeBytes(t, value, inspected.ranges.info)
	if !bytes.Equal(infoRange, infoChunk) {
		t.Fatalf("LIST/INFO range = %x, want %x", infoRange, infoChunk)
	}
	if !bytes.Contains(infoRange, unknownField) {
		t.Fatalf("unknown INFO field was not in the source range: %x", infoRange)
	}

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
	input := stream.MustDescriptor("wave", access.Bytes().Descriptor(), timing.Base{}, property.New()).WithMetadata(providerDocument)
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

func TestMuxRestoresRIFFInfoAndUnknownChunkPlacement(t *testing.T) {
	beforeFormat := waveTestChunk(t, "PRE!", []byte{1, 2, 3}, 0x91)
	formatChunk := waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0)
	unknownField := infoTestChunk(t, "XTRA", []byte{4, 5, 6}, 0xa2)
	infoChunk := infoTestList(t, infoTestChunk(t, "INAM", []byte("Song\x00"), 0), unknownField)
	data := []byte{7, 8}
	dataChunk := waveTestChunk(t, tagDATA, data, 0)
	afterData := waveTestChunk(t, "POST", []byte{9, 10, 11}, 0xb3)
	original := waveTestRIFF(t, beforeFormat, formatChunk, infoChunk, dataChunk, afterData)
	resolver := infoTestResolver(t)

	inspected, err := inspectHeaderWithMetadata(t.Context(), memoryRandom(original), resolver)
	if err != nil {
		t.Fatal(err)
	}
	header, err := newRangeMuxHeader(inspected.description, inspected)
	if err != nil {
		t.Fatal(err)
	}
	compileContext, err := metadata.WithResolver(plugin.CompileContextWithContext(plugin.CompileContext{}, t.Context()), resolver)
	if err != nil {
		t.Fatal(err)
	}
	compileContext, err = mediaformat.WithInspection(compileContext, mediaformat.NewInspection(WAVE(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	properties, err := inspected.description.Properties()
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor("wave", codec.Packets().Descriptor(), timing.MustBase(1, int64(inspected.description.Rate)), properties).WithMetadata(inspected.metadata)
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, compileContext, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
	if err != nil {
		t.Fatal(err)
	}
	if got := int(compiled.Resources().Memory); got != max(header.payloadBytes(), wavePageSize) {
		t.Fatalf("mux payload grant = %d, want %d", got, max(header.payloadBytes(), wavePageSize))
	}
	encoded := materializeRangeMuxHeader(t, header, original, data)
	roundTrip, err := inspectHeaderWithMetadata(t.Context(), memoryRandom(encoded), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded[roundTrip.dataOffset:int64(roundTrip.dataOffset)+int64(roundTrip.dataSize)], data) {
		t.Fatalf("round-trip PCM payload = %x", encoded[roundTrip.dataOffset:int64(roundTrip.dataOffset)+int64(roundTrip.dataSize)])
	}
	if got := sourceRangeBytes(t, encoded, roundTrip.ranges.beforeFormat); !bytes.Equal(got, beforeFormat) {
		t.Fatalf("round-trip before-format range = %x, want %x", got, beforeFormat)
	}
	if got := sourceRangeBytes(t, encoded, roundTrip.ranges.beforeData); !bytes.Equal(got, infoChunk) {
		t.Fatalf("round-trip before-data range = %x, want %x", got, infoChunk)
	}
	if got := sourceRangeBytes(t, encoded, roundTrip.ranges.afterData); !bytes.Equal(got, afterData) {
		t.Fatalf("round-trip after-data range = %x, want %x", got, afterData)
	}
}

func TestMuxCompilePropagatesCancellationToMetadataMarshal(t *testing.T) {
	started := make(chan struct{})
	hidden, canceled := false, false
	encoding := plugin.NewComponent[cancelInfoEncodingID](
		plugin.Descriptor{DisplayName: "canceling RIFF INFO encoding"},
		configurationSchema(),
		metadata.WithEncoding(
			func(ctx metadata.ParseContext) (metadata.Document, error) {
				return metadata.NewBuilder(ctx.Scope()).Build()
			},
			func(ctx metadata.MarshalContext) (metadata.Blob, error) {
				hidden = ctx.Context().Value(cancelInfoContextKey{}) == nil
				close(started)
				select {
				case <-ctx.Context().Done():
					canceled = errors.Is(ctx.Context().Err(), context.Canceled)
					return metadata.Blob{}, ctx.Context().Err()
				case <-time.After(time.Second):
					return metadata.Blob{}, errors.New("metadata Marshal cancellation was not propagated")
				}
			},
		),
	)
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{RIFFInfo(): encoding})
	if err != nil {
		t.Fatal(err)
	}
	blockID := newChunkBlockID(48, chunkBeforeData, chunkInfo)
	document, err := metadata.NewBuilder(metadata.StreamScope).
		AddBlock(metadata.NewRawBlock(blockID, RIFFInfo(), encoding.Identity(), metadata.NewBlob("application/x-riff-info", infoTestList(t)))).
		Build()
	if err != nil {
		t.Fatal(err)
	}
	compileContext, err := metadata.WithResolver(plugin.CompileContext{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), cancelInfoContextKey{}, "hidden"))
	compileContext = plugin.CompileContextWithContext(compileContext, parent)
	go func() {
		select {
		case <-started:
		case <-time.After(time.Second):
		}
		cancel()
	}()

	_, err = marshalMuxChunks(compileContext.Context(), resolver, document)
	if err == nil || !hidden || !canceled {
		t.Fatalf("metadata Marshal cancellation = %v, value hidden %v, canceled %v", err, hidden, canceled)
	}
}

func restoredWaveChunks(document metadata.Document) [][]byte {
	var result [][]byte
	for _, block := range document.Blocks() {
		if _, ok := parseChunkBlockID(block.ID()); !ok {
			continue
		}
		payload := block.Payload().AppendTo(nil)
		// Only the writer's own empty reservation is dropped. An input-derived
		// reservation carries content and stays in the comparison.
		if bytes.Equal(payload, reserveChunkOf(muxChunks{})) {
			continue
		}
		result = append(result, payload)
	}
	return result
}

func materializeRangeMuxHeader(t testing.TB, header muxHeader, source, data []byte) []byte {
	t.Helper()
	if !header.rangeMode {
		t.Fatalf("range materializer received a legacy header")
	}
	value := make([]byte, 0, len(header.prefix)+8+ds64PayloadSize+len(header.format)+len(header.dataTag)+len(data))
	value = append(value, header.prefix...)
	if header.ranges.reservation.valid() {
		value = append(value, sourceRangeBytes(t, source, header.ranges.reservation)...)
	} else {
		value = append(value, reserveChunkOf(muxChunks{})...)
	}
	value = append(value, sourceRangeBytes(t, source, header.ranges.beforeFormat)...)
	value = append(value, header.format...)
	value = append(value, sourceRangeBytes(t, source, header.ranges.beforeData)...)
	value = append(value, header.dataTag...)
	value = append(value, data...)
	if len(data)&1 != 0 {
		value = append(value, 0)
	}
	value = append(value, sourceRangeBytes(t, source, header.ranges.afterData)...)
	value = append(value, sourceRangeBytes(t, source, header.ranges.trailer)...)
	finalized, err := header.finalize(uint64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range finalized.patches {
		copy(value[patch.offset:], patch.payload)
	}
	return value
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

func sourceRangeBytes(t testing.TB, source []byte, value sourceRange) []byte {
	t.Helper()
	if value.length > uint64(len(source)) || value.offset > uint64(len(source))-value.length {
		t.Fatalf("source range %#v is outside %d-byte fixture", value, len(source))
	}
	return append([]byte(nil), source[value.offset:value.offset+value.length]...)
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

// Every JUNK chunk is input-derived content. The one in the ds64 reservation
// slot is recorded under its own anchor so the writer can put it back in the
// same slot instead of appending a second copy after its own reservation.
func TestInspectAnchorsTheDs64ReservationSlot(t *testing.T) {
	formatChunk := waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0)
	dataChunk := waveTestChunk(t, tagDATA, []byte{7, 8}, 0)
	reservationPayload := bytes.Repeat([]byte{0x5a}, ds64PayloadSize)

	for _, test := range []struct {
		name   string
		value  []byte
		anchor chunkAnchor
	}{
		{
			name:   "reservation-slot",
			value:  waveTestRIFF(t, waveTestChunk(t, tagJUNK, reservationPayload, 0), formatChunk, dataChunk),
			anchor: chunkReservation,
		},
		{
			name:   "same-size-after-format",
			value:  waveTestRIFF(t, formatChunk, waveTestChunk(t, tagJUNK, reservationPayload, 0), dataChunk),
			anchor: chunkBeforeData,
		},
		{
			name:   "other-size-in-slot",
			value:  waveTestRIFF(t, waveTestChunk(t, tagJUNK, make([]byte, ds64PayloadSize+2), 0), formatChunk, dataChunk),
			anchor: chunkBeforeFormat,
		},
		{
			name:   "odd-size-in-slot",
			value:  waveTestRIFF(t, waveTestChunk(t, tagJUNK, make([]byte, ds64PayloadSize+1), 0xc4), formatChunk, dataChunk),
			anchor: chunkBeforeFormat,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspected, err := inspectHeaderWithMetadata(t.Context(), memoryRandom(test.value), infoTestResolver(t))
			if err != nil {
				t.Fatal(err)
			}
			if got := inspected.ranges.rangeFor(test.anchor); !got.valid() {
				t.Fatalf("preserved anchor range = %#v, want %v", got, test.anchor)
			}
		})
	}
}

// A non-zero reservation slot is legal RIFF and is content the source owns.
// Writing RIFF back must reproduce it byte for byte, and doing it again must
// not change or duplicate anything.
func TestMuxRestoresANonZeroReservationSlot(t *testing.T) {
	reservationPayload := bytes.Repeat([]byte{0x5a}, ds64PayloadSize)
	reservation := waveTestChunk(t, tagJUNK, reservationPayload, 0)
	formatChunk := waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0)
	data := []byte{7, 8}
	original := waveTestRIFF(t, reservation, formatChunk, waveTestChunk(t, tagDATA, data, 0))
	resolver := infoTestResolver(t)

	encoded := original
	for pass := 0; pass < 2; pass++ {
		inspected, err := inspectHeaderWithMetadata(t.Context(), memoryRandom(encoded), resolver)
		if err != nil {
			t.Fatalf("pass %d inspect failed: %v", pass, err)
		}
		if got := sourceRangeBytes(t, encoded, inspected.ranges.reservation); !bytes.Equal(got, reservation) {
			t.Fatalf("pass %d reservation range = %x, want %x", pass, got, reservation)
		}
		header, err := newRangeMuxHeader(inspected.description, inspected)
		if err != nil {
			t.Fatalf("pass %d header failed: %v", pass, err)
		}
		encoded = materializeRangeMuxHeader(t, header, encoded, data)
		if !bytes.Equal(encoded, original) {
			t.Fatalf("pass %d round trip = %x, want %x", pass, encoded, original)
		}
	}
}

// Promoting to RF64 puts ds64 in the reservation slot. The header layout is
// fixed before the data size is known, so the preserved bytes have nowhere
// else to go and the loss is part of the contract rather than a surprise.
func TestRF64PromotionReplacesThePreservedReservation(t *testing.T) {
	reservationPayload := bytes.Repeat([]byte{0x5a}, ds64PayloadSize)
	chunks := muxChunks{reservation: waveTestChunk(t, tagJUNK, reservationPayload, 0)}
	description := sample.Description{Coding: sample.S16, Packing: sample.Interleaved, Endian: sample.LittleEndian, Rate: 48_000, Layout: sample.Mono(), ValidBits: 16}
	header, err := newMuxHeaderWithChunks(description, chunks)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(header.initial[reserveOffset:reserveOffset+8+ds64PayloadSize], chunks.reservation) {
		t.Fatalf("reserved slot = %x, want the preserved chunk", header.initial[reserveOffset:reserveOffset+8+ds64PayloadSize])
	}
	finalized, err := header.finalize(uint64(math.MaxUint32) + 2)
	if err != nil {
		t.Fatal(err)
	}
	if !finalized.rf64 {
		t.Fatal("an oversized data chunk did not promote to RF64")
	}
	for _, patch := range finalized.patches {
		if patch.offset == int64(reserveOffset) && string(patch.payload[0:4]) == tagDS64 {
			return
		}
	}
	t.Fatalf("RF64 finalization did not overwrite the reservation slot: %#v", finalized.patches)
}

// Appended tags and encoder padding past the RIFF chunk are common. The spec
// makes the size field the chunk boundary, not the file boundary, so the
// region is preserved and written back outside the RIFF size.
func TestInspectPreservesBytesPastTheRIFFChunk(t *testing.T) {
	formatChunk := waveTestChunk(t, tagFMT, pcmFormat(1, 48_000, 16), 0)
	dataChunk := waveTestChunk(t, tagDATA, []byte{7, 8}, 0)
	value := waveTestRIFF(t, formatChunk, dataChunk)
	trailer := []byte("ID3\x04appended")
	source := append(append([]byte(nil), value...), trailer...)

	inspected, err := inspectHeaderWithSize(t.Context(), memoryRandom(source), uint64(len(source)), true, infoTestResolver(t), 1<<20)
	if err != nil {
		t.Fatalf("trailing bytes rejected the stream: %v", err)
	}
	if blocks := inspected.metadata.Blocks(); len(blocks) != 0 {
		t.Fatalf("inspection retained trailing Blob blocks = %d", len(blocks))
	}
	if got := sourceRangeBytes(t, source, inspected.ranges.trailer); !bytes.Equal(got, trailer) {
		t.Fatalf("preserved trailer range = %q, want %q", got, trailer)
	}

	header, err := newRangeMuxHeader(inspected.description, inspected)
	if err != nil {
		t.Fatal(err)
	}
	// The trailing region sits past the RIFF chunk, so it must not change the
	// size the header declares.
	plain, err := newMuxHeaderWithChunks(inspected.description, muxChunks{})
	if err != nil {
		t.Fatal(err)
	}
	withTrailer, err := header.finalize(4)
	if err != nil {
		t.Fatal(err)
	}
	without, err := plain.finalize(4)
	if err != nil {
		t.Fatal(err)
	}
	if withTrailer.fileSize != without.fileSize {
		t.Fatalf("RIFF-scoped size with trailer = %d, without = %d", withTrailer.fileSize, without.fileSize)
	}
}
