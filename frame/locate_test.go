package frame

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

func TestLocateFrameSkipsJunkAndFalseSync(t *testing.T) {
	t.Parallel()
	info := streaminfo.StreamInfo{MinBlockSize: 16, MaxBlockSize: 16, SampleRate: 8000, Channels: 1, BitsPerSample: 16}
	first := testFrame(t, info, 0, []byte{1, 0xff, 0xf8, 2})
	second := testFrame(t, info, 1, []byte{3, 4})
	input := append([]byte{9, 8, 7}, first...)
	input = append(input, second...)
	reader := bytes.NewReader(input)
	data, header, scanner, err := LocateFrame(reader, info)
	if err != nil {
		t.Fatal(err)
	}
	if header.Number != 0 || !bytes.Equal(data, first) {
		t.Fatalf("located frame = number %d, data %x", header.Number, data)
	}
	next, nextHeader, err := scanner.Next()
	if err != nil || nextHeader.Number != 1 || !bytes.Equal(next, second) {
		t.Fatalf("next frame = (%d, %x, %v)", nextHeader.Number, next, err)
	}
}

func TestLocateFrameNotFound(t *testing.T) {
	t.Parallel()
	info := streaminfo.StreamInfo{MinBlockSize: 16, MaxBlockSize: 16, SampleRate: 8000, Channels: 1, BitsPerSample: 16}
	_, _, _, err := LocateFrame(bytes.NewReader([]byte{1, 2, 3}), info)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("LocateFrame() error = %v, want ErrUnexpectedEOF", err)
	}
}

func testFrame(t testing.TB, info streaminfo.StreamInfo, number uint64, payload []byte) []byte {
	t.Helper()
	w := bits.NewWriter()
	header := &Header{BlockSize: 16, SampleRate: info.SampleRate, Channels: 1, BitsPerSample: 16, Number: number}
	if err := EncodeHeader(w, header, false); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), w.Bytes()...)
	data = append(data, payload...)
	crc := hash.CRC16(data)
	return append(data, byte(crc>>8), byte(crc))
}
