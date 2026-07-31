package layer3

import (
	"testing"

	"github.com/godexture/sdk/bits"
)

func BenchmarkReservoirRestoreSave(b *testing.B) {
	frameData := make([]byte, 2304)
	var decoder Decoder
	var workspace Workspace
	var frameReader bits.Reader
	decoder.Init()
	decoder.reservoir.Grow(maxBitReservoirBytes)

	b.ReportAllocs()
	for b.Loop() {
		frameReader.Init(frameData, 0, int32(len(frameData)*8))
		if err := restoreReservoir(&decoder, &frameReader, &workspace, maxBitReservoirBytes); err != nil {
			b.Fatal(err)
		}
		workspace.bitReader.Seek(int32(len(frameData) * 8))
		saveReservoir(&decoder, &workspace)
	}
}
