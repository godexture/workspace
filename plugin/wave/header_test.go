package wave

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type memoryRandom []byte

func (r memoryRandom) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(r)) {
		return 0, io.EOF
	}
	count := copy(destination, r[offset:])
	if count != len(destination) {
		return count, io.EOF
	}
	return count, nil
}

type testChunk struct {
	id      string
	payload []byte
}

func TestInspectHeaderReadsRIFFAndRF64PCM(t *testing.T) {
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	riff := testWAVE(data, 2, 48_000, testChunk{id: "JUNK", payload: []byte{0xff}})
	value, err := inspectHeader(context.Background(), memoryRandom(riff))
	if err != nil {
		t.Fatal(err)
	}
	want := sample.Description{Format: sample.S16Interleaved, ValidBits: 16, Rate: 48_000, Layout: sample.Stereo, Endian: sample.LittleEndian}
	if value.description != want || value.dataOffset != 54 || value.dataSize != uint64(len(data)) || value.blockAlign != 4 || value.rf64 {
		t.Fatalf("RIFF inspection = %#v", value)
	}

	rf64 := testRF64(data, 2, 48_000)
	value, err = inspectHeader(context.Background(), memoryRandom(rf64))
	if err != nil {
		t.Fatal(err)
	}
	if value.description != want || value.dataSize != uint64(len(data)) || !value.rf64 {
		t.Fatalf("RF64 inspection = %#v", value)
	}
}

func TestInspectHeaderUsesStableSizeForRIFFAndRF64(t *testing.T) {
	for name, complete := range map[string][]byte{
		"riff": testWAVE([]byte{1, 2, 3, 4}, 1, 48_000),
		"rf64": testRF64([]byte{1, 2, 3, 4}, 1, 48_000),
	} {
		t.Run(name, func(t *testing.T) {
			truncated := complete[:len(complete)-2]
			_, err := inspectHeaderWithSize(context.Background(), memoryRandom(truncated), uint64(len(truncated)), true, metadata.Resolver{}, job.DefaultBudget().InspectMemory)
			if !errors.Is(err, ErrTruncatedData) {
				t.Fatalf("stable-size inspection error = %v", err)
			}
			if _, err := inspectHeader(context.Background(), memoryRandom(truncated)); err != nil {
				t.Fatalf("inspection without StableSize should defer payload truncation: %v", err)
			}
		})
	}
}

func TestInspectHeaderRejectsMalformedAndUnsupportedStreams(t *testing.T) {
	valid := testWAVE([]byte{1, 2, 3, 4}, 1, 48_000)
	badSignature := append([]byte(nil), valid...)
	copy(badSignature[0:4], "NOPE")
	partialBlock := testWAVE([]byte{1, 2, 3}, 1, 48_000)
	unsupported := testWAVEWithFormat([]byte{1, 2, 3}, pcmFormat(1, 48_000, 24))
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{name: "signature", data: badSignature, want: ErrMalformed},
		{name: "truncated", data: valid[:18], want: ErrMalformed},
		{name: "partial block", data: partialBlock, want: ErrMalformed},
		{name: "sample format", data: unsupported, want: ErrUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := inspectHeader(context.Background(), memoryRandom(test.data))
			if !errors.Is(err, test.want) {
				t.Fatalf("Inspect error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDemuxCompileUsesInspectionInsteadOfCarrierProperties(t *testing.T) {
	data := testWAVE([]byte{1, 2, 3, 4}, 1, 32_000)
	inspected, err := inspectHeader(context.Background(), memoryRandom(data))
	if err != nil {
		t.Fatal(err)
	}
	compileContext, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewClonedInspection(WAVE(), inspected, func(value header) header { return value }))
	if err != nil {
		t.Fatal(err)
	}
	component := demuxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	carrier := stream.MustDescriptor("wave", access.Bytes().Descriptor(), timing.Base{}, property.New())
	compiled, err := plugin.Compile(component, compileContext, resolved, flow.NewDescriptors(flow.Describe("bytes", carrier)))
	if err != nil {
		t.Fatal(err)
	}
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok {
		t.Fatal("WAVE outputs have the wrong descriptor type")
	}
	output, ok := outputs.One("chunks")
	if !ok || output.Schema() != mediaformat.Chunks().Identity() || output.TimeBase() != timing.MustBase(1, 32_000) {
		t.Fatalf("WAVE output = %#v", output)
	}
	description, err := sample.FromProperties(output.Properties())
	if err != nil || description != inspected.description {
		t.Fatalf("WAVE properties = %#v, %v", description, err)
	}
	if tag, ok := codec.TagOf(output.Properties()); !ok || tag != PCMTag() {
		t.Fatalf("WAVE codec tag = %q/%v", tag, ok)
	}
	if compiled.Resources().Memory != 2 {
		t.Fatalf("WAVE reframe memory = %d", compiled.Resources().Memory)
	}
}

func testWAVE(data []byte, channels uint16, rate uint32, extra ...testChunk) []byte {
	chunks := append([]testChunk(nil), extra...)
	chunks = append(chunks, testChunk{id: tagFMT, payload: pcmFormat(channels, rate, 16)}, testChunk{id: tagDATA, payload: data})
	return testRIFF(tagRIFF, chunks, false)
}

func testWAVEWithFormat(data, format []byte) []byte {
	return testRIFF(tagRIFF, []testChunk{{id: tagFMT, payload: format}, {id: tagDATA, payload: data}}, false)
}

func testRF64(data []byte, channels uint16, rate uint32) []byte {
	ds64 := make([]byte, 28)
	binary.LittleEndian.PutUint64(ds64[8:16], uint64(len(data)))
	binary.LittleEndian.PutUint64(ds64[16:24], uint64(len(data))/uint64(channels*2))
	value := testRIFF(tagRF64, []testChunk{
		{id: tagDS64, payload: ds64},
		{id: tagFMT, payload: pcmFormat(channels, rate, 16)},
		{id: tagDATA, payload: data},
	}, true)
	binary.LittleEndian.PutUint64(value[20:28], uint64(len(value)-8))
	return value
}

func testRIFF(root string, chunks []testChunk, rf64 bool) []byte {
	var body bytes.Buffer
	body.WriteString(tagWAVE)
	for _, chunk := range chunks {
		body.WriteString(chunk.id)
		size := uint32(len(chunk.payload))
		if rf64 && chunk.id == tagDATA {
			size = ^uint32(0)
		}
		_ = binary.Write(&body, binary.LittleEndian, size)
		body.Write(chunk.payload)
		if len(chunk.payload)&1 != 0 {
			body.WriteByte(0)
		}
	}
	var result bytes.Buffer
	result.WriteString(root)
	size := uint32(body.Len())
	if rf64 {
		size = ^uint32(0)
	}
	_ = binary.Write(&result, binary.LittleEndian, size)
	result.Write(body.Bytes())
	return result.Bytes()
}

func pcmFormat(channels uint16, rate uint32, bits uint16) []byte {
	blockAlign := channels * ((bits + 7) / 8)
	value := make([]byte, 16)
	binary.LittleEndian.PutUint16(value[0:2], formatPCM)
	binary.LittleEndian.PutUint16(value[2:4], channels)
	binary.LittleEndian.PutUint32(value[4:8], rate)
	binary.LittleEndian.PutUint32(value[8:12], rate*uint32(blockAlign))
	binary.LittleEndian.PutUint16(value[12:14], blockAlign)
	binary.LittleEndian.PutUint16(value[14:16], bits)
	return value
}

// A declared chunk size is content the source controls, so preserving it must
// not size an allocation beyond the retained inspection-memory budget.
func TestInspectRefusesToPreserveChunksBeyondTheMemoryBudget(t *testing.T) {
	payload := make([]byte, 4096)
	value := testWAVE([]byte{1, 0, 2, 0}, 1, 48_000, testChunk{id: "BULK", payload: payload})
	if _, err := inspectHeaderWithSize(context.Background(), memoryRandom(value), uint64(len(value)), true, metadata.Resolver{}, 1<<20); err != nil {
		t.Fatalf("generous budget rejected a preserved chunk: %v", err)
	}
	_, err := inspectHeaderWithSize(context.Background(), memoryRandom(value), uint64(len(value)), true, metadata.Resolver{}, 512)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("narrow budget error = %v, want %v", err, ErrUnsupported)
	}
}
