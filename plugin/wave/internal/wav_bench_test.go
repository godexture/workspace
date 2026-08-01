package internal

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func BenchmarkWAVReadMP3Packets(b *testing.B) {
	mp3Data, err := os.ReadFile("../../../plugin/mp3/test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		b.Fatal(err)
	}
	wavData := buildWAVWithFormatTag(b, wavAudioMP3, 0, 2, 44100, 1, mp3Data)

	b.ReportAllocs()
	b.SetBytes(int64(len(mp3Data)))
	for b.Loop() {
		demuxer, err := NewDemuxer(bytes.NewReader(wavData), DemuxerConfig{})
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err := demuxer.Analyze(); err != nil {
			b.Fatal(err)
		}
		for {
			pkt, _, err := demuxer.ReadPacket()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
			pkt.Release()
		}
	}
}
