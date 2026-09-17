package mp3

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/id3"
	"github.com/godexture/godec/plugin/mp3/internal/frame"
	"github.com/godexture/godec/resource"
)

type mp3Source struct {
	data []byte
	size int64
}

type cancelEncodingID struct{}
type cancelConfigID struct{}

func (*mp3Source) Close() error { return nil }

func (*mp3Source) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.RandomRead, access.StableSize)
	return value
}

func (s *mp3Source) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[offset:])
	if count != len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *mp3Source) Size(context.Context) (int64, error) {
	if s.size != 0 {
		return s.size, nil
	}
	return int64(len(s.data)), nil
}

func mp3Opening(t testing.TB, data []byte) access.Opening {
	t.Helper()
	source := &mp3Source{data: data}
	selection, ok := access.Select(source.Capabilities(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)))
	if !ok {
		t.Fatal("MP3 test source did not satisfy random stable requirements")
	}
	opening, err := access.NewOpening(access.SourceDirection, source, selection, 0)
	if err != nil {
		t.Fatal(err)
	}
	return opening
}

func inspectTest(t testing.TB, data []byte, prepared plugin.CompileContext, read, memory resource.Bytes) inspection {
	t.Helper()
	value, err := inspectMP3(format.NewInspectContext(t.Context(), mp3Opening(t, data), prepared, read, memory))
	if err != nil {
		t.Fatal(err)
	}
	preparedInspection, ok := format.InspectionOf[inspection](mustInspectionContext(t, value), MP3())
	if !ok {
		t.Fatal("MP3 inspection did not carry its value")
	}
	return preparedInspection
}

func mustInspectionContext(t testing.TB, value format.Inspection) plugin.CompileContext {
	t.Helper()
	prepared, err := format.WithInspection(plugin.CompileContext{}, value)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func mp3Header(bitrate byte, rate byte, padding bool, mode frame.ChannelMode) [4]byte {
	var raw [4]byte
	raw[0] = 0xff
	raw[1] = 0xe0 | 3<<3 | 1<<1 | 1
	raw[2] = bitrate<<4 | rate<<2
	if padding {
		raw[2] |= 2
	}
	raw[3] = byte(mode) << 6
	return raw
}

func mp3Frame(header [4]byte, size int, fill byte) []byte {
	value := bytes.Repeat([]byte{fill}, size)
	copy(value, header[:])
	return value
}

func mp3Audio(header [4]byte, count int) []byte {
	parsed, err := frame.Parse(header[:])
	if err != nil {
		panic(err)
	}
	size, err := parsed.FrameSize(0)
	if err != nil {
		panic(err)
	}
	result := mp3Frame(header, size, 0)
	for index := 1; index < count; index++ {
		result = append(result, mp3Frame(header, size, byte(index))...)
	}
	return result
}

func id3v2(version, flags byte, body []byte, footer bool) []byte {
	header := make([]byte, 10)
	copy(header, []byte("ID3"))
	header[3], header[4], header[5] = version, 0, flags
	size := len(body)
	header[6] = byte(size >> 21)
	header[7] = byte(size >> 14 & 0x7f)
	header[8] = byte(size >> 7 & 0x7f)
	header[9] = byte(size & 0x7f)
	result := append(header, body...)
	if footer {
		value := append([]byte("3DI"), header[3:]...)
		result = append(result, value...)
	}
	return result
}

func id3v1() []byte {
	value := make([]byte, 128)
	copy(value, []byte("TAG"))
	return value
}

func id3Prepared(t testing.TB) plugin.CompileContext {
	t.Helper()
	v1 := id3Component(t, id3.V1EncodingIdentity())
	v2 := id3Component(t, id3.V2EncodingIdentity())
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{LeadingID3v2(): v2, TrailingID3v1(): v1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := metadata.WithResolver(plugin.CompileContext{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func id3Component(t testing.TB, identity plugin.Identity) plugin.Component {
	t.Helper()
	for _, component := range id3.Plugin().Components() {
		if component.Identity() == identity {
			return component
		}
	}
	t.Fatalf("missing ID3 component %s", identity)
	return plugin.Component{}
}

func TestProbeMP3(t *testing.T) {
	need, err := probeMP3(format.NewProbeContext(t.Context(), nil))
	if err != nil || need.Status() != format.ProbeNeedsData || len(need.Needs()) != 1 || need.Needs()[0].Length() != 4 {
		t.Fatalf("empty probe = %#v, %v", need, err)
	}
	header := mp3Header(9, 0, false, frame.Stereo)
	match, err := probeMP3(format.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView(header[:])}, 4))
	if err != nil || match.Status() != format.ProbeMatch {
		t.Fatalf("MPEG probe = %#v, %v", match, err)
	}
	nonMatch, err := probeMP3(format.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView([]byte("RIFF"))}, 4))
	if err != nil || nonMatch.Status() != format.ProbeMismatch {
		t.Fatalf("mismatch probe = %#v, %v", nonMatch, err)
	}
	tagPrefix := []byte("ID3")
	needTag, err := probeMP3(format.NewProbeContext(t.Context(), []access.ProbeView{access.NewProbeView(tagPrefix)}))
	if err != nil || needTag.Status() != format.ProbeNeedsData || needTag.Needs()[0].Length() != 10 {
		t.Fatalf("ID3 prefix probe = %#v, %v", needTag, err)
	}
	matchTag, err := probeMP3(format.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView(id3v2(4, 0, nil, false))}, 10))
	if err != nil || matchTag.Status() != format.ProbeMatch {
		t.Fatalf("ID3 probe = %#v, %v", matchTag, err)
	}
	malformed, err := probeMP3(format.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView([]byte("ID3\x04\x00\x00\x80\x00\x00\x00"))}, 10))
	if err != nil || malformed.Status() != format.ProbeMalformed {
		t.Fatalf("truncated ID3 probe = %#v, %v", malformed, err)
	}
}

func TestInspectCBRIsUnknownAndMemoryUsesActualAudioLength(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	audio := mp3Audio(header, 1)
	first, err := frame.Parse(header[:])
	if err != nil {
		t.Fatal(err)
	}
	firstSize, err := first.FrameSize(0)
	if err != nil {
		t.Fatal(err)
	}
	workspace := len(audio)
	value := inspectTest(t, audio, plugin.CompileContext{}, 1<<20, resource.Bytes(workspace))
	if value.hasDuration {
		t.Fatal("CBR inspection published a duration")
	}
	if value.audioStart != 0 || value.audioEnd != int64(len(audio)) || value.firstBytes != firstSize {
		t.Fatalf("CBR inspection = %#v", value)
	}
}

func TestInspectXingVBRIAndFreeFormat(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	xing := mp3Frame(header, 417, 0)
	copy(xing[36:40], []byte("Xing"))
	binary.BigEndian.PutUint32(xing[40:44], 1)
	binary.BigEndian.PutUint32(xing[44:48], 100)
	xingValue := inspectTest(t, xing, plugin.CompileContext{}, 1<<20, 1<<20)
	if !xingValue.hasDuration || xingValue.duration != timing.NewDuration(100*1152) {
		t.Fatalf("Xing duration = %v/%v", xingValue.duration, xingValue.hasDuration)
	}

	vbri := mp3Frame(header, 417, 0)
	copy(vbri[36:40], []byte("VBRI"))
	binary.BigEndian.PutUint16(vbri[40:42], 1)
	binary.BigEndian.PutUint16(vbri[58:60], 1)
	binary.BigEndian.PutUint16(vbri[60:62], 1)
	binary.BigEndian.PutUint32(vbri[50:54], 75)
	vbriValue := inspectTest(t, vbri, plugin.CompileContext{}, 1<<20, 1<<20)
	if !vbriValue.hasDuration || vbriValue.duration != timing.NewDuration(75*1152) {
		t.Fatalf("VBRI duration = %v/%v", vbriValue.duration, vbriValue.hasDuration)
	}

	freeHeader := mp3Header(0, 0, false, frame.Stereo)
	free := append(mp3Frame(freeHeader, 300, 0), mp3Frame(freeHeader, 300, 1)...)
	free = append(free, mp3Frame(freeHeader, 300, 2)...)
	freeValue := inspectTest(t, free, plugin.CompileContext{}, 1<<20, 1<<20)
	if freeValue.freeBytes != 300 || freeValue.maxFrameBytes != 301 {
		t.Fatalf("free-format framing = %d/%d", freeValue.freeBytes, freeValue.maxFrameBytes)
	}
	if _, err := inspectMP3(format.NewInspectContext(t.Context(), mp3Opening(t, free), plugin.CompileContext{}, 1<<20, resource.Bytes(len(free)))); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("free-format growth budget error = %v", err)
	}
}

func TestInspectID3BindingAndBudgets(t *testing.T) {
	header := mp3Header(9, 0, false, frame.Stereo)
	audio := mp3Audio(header, 2)
	data := append(id3v2(2, 0, []byte("ab"), false), audio...)
	data = append(data, id3v1()...)
	if _, err := inspectMP3(format.NewInspectContext(t.Context(), mp3Opening(t, data), plugin.CompileContext{}, 1<<20, 1<<20)); err == nil {
		t.Fatal("inspection without an ID3 binding succeeded")
	}
	prepared := id3Prepared(t)
	value := inspectTest(t, data, prepared, 1<<20, 1<<20)
	if value.audioStart != int64(len(id3v2(2, 0, []byte("ab"), false))) || value.audioEnd != int64(len(data)-128) || !value.metadata.Valid() {
		t.Fatalf("tag ranges/metadata = %#v", value)
	}
	_, err := inspectMP3(format.NewInspectContext(t.Context(), mp3Opening(t, data), prepared, 10, 1<<20))
	if !errors.Is(err, ErrUnsupported) || !errors.Is(err, errInspectReadBudget) || errors.Is(err, ErrMalformed) {
		t.Fatalf("read budget error = %v", err)
	}
	_, err = inspectMP3(format.NewInspectContext(t.Context(), mp3Opening(t, data), prepared, 1<<20, 10))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("memory budget error = %v", err)
	}
}

func TestInspectRejectsMalformedID3Footer(t *testing.T) {
	data := id3v2(4, 0x10, nil, true)
	data[len(data)-1] ^= 1
	data = append(data, mp3Audio(mp3Header(9, 0, false, frame.Stereo), 1)...)
	if _, err := inspectMP3(format.NewInspectContext(t.Context(), mp3Opening(t, data), id3Prepared(t), 1<<20, 1<<20)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed footer error = %v", err)
	}
}

func TestInspectPropagatesCancellationFromMetadataParser(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	component := plugin.NewComponent[cancelEncodingID](
		plugin.Descriptor{DisplayName: "canceling metadata parser"},
		config.Struct[cancelConfigID](func() struct{} { return struct{}{} }).Version("1").Build(),
		metadata.WithEncoding(
			func(parse metadata.ParseContext) (metadata.Document, error) {
				cancel()
				return metadata.NewBuilder(parse.Scope()).Build()
			},
			func(metadata.MarshalContext) (metadata.Blob, []loss.Loss, error) {
				return metadata.NewBlob("application/octet-stream", nil), nil, nil
			},
			tag.Title().Erased(),
		),
	)
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{LeadingID3v2(): component}, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := metadata.WithResolver(plugin.CompileContext{}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	data := append(id3v2(2, 0, nil, false), mp3Audio(mp3Header(9, 0, false, frame.Stereo), 1)...)
	_, err = inspectMP3(format.NewInspectContext(ctx, mp3Opening(t, data), prepared, 1<<20, 1<<20))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled metadata inspection error = %v", err)
	}
}

func TestInspectDoesNotClassifyCancellationAsMalformedMedia(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	data := mp3Audio(mp3Header(9, 0, false, frame.Stereo), 1)
	_, err := inspectMP3(format.NewInspectContext(ctx, mp3Opening(t, data), plugin.CompileContext{}, 1<<20, 1<<20))
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrMalformed) {
		t.Fatalf("cancelled source read error = %v", err)
	}
}
