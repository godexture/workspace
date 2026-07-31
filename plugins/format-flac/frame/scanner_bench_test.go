package frame

import (
	"bytes"
	"testing"

	"github.com/godexture/godec/plugins/format-flac/streaminfo"
)

func BenchmarkScannerNext(b *testing.B) {
	info := benchmarkStreamInfo(b)
	frame := decodeHex(b, appendixDFrame)

	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	for b.Loop() {
		scanner, err := NewScanner(bytes.NewReader(frame), info, Options{})
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := scanner.Next(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScannerResync(b *testing.B) {
	info := benchmarkStreamInfo(b)
	frame := decodeHex(b, appendixDFrame)
	const maxFrameSize = 256 << 10
	stream := make([]byte, 0, len(frame)*2+maxFrameSize+1)
	stream = append(stream, frame...)
	stream = append(stream, make([]byte, maxFrameSize+1)...)
	stream = append(stream, frame...)

	b.ReportAllocs()
	b.SetBytes(int64(len(stream)))
	for b.Loop() {
		scanner, err := NewScanner(bytes.NewReader(stream), info, Options{Sync: true})
		if err != nil {
			b.Fatal(err)
		}
		scanner.maxFrameSize = maxFrameSize
		if _, _, err := scanner.Next(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkStreamInfo(b *testing.B) streaminfo.StreamInfo {
	b.Helper()
	info, err := streaminfo.Parse(decodeHex(b, appendixDStreamInfo))
	if err != nil {
		b.Fatal(err)
	}
	return info
}
