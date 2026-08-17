package linear

import (
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

var benchmarkPCMSum uint64

func BenchmarkPCMReadViews(b *testing.B) {
	const samples = 4096
	encoded := make([]byte, samples*2)
	directSamples := make([]int16, samples)
	for index := range samples {
		value := uint16(index*31 + 7)
		binary.LittleEndian.PutUint16(encoded[index*2:], value)
		directSamples[index] = int16(value)
	}
	allocator, err := buffer.NewAllocator(int64(len(encoded) * 2))
	if err != nil {
		b.Fatal(err)
	}
	encodedHandle, err := allocator.FromBytes(encoded, 2)
	if err != nil {
		b.Fatal(err)
	}
	defer encodedHandle.Release()
	planeHandle, err := allocator.FromBytes(encoded, 2)
	if err != nil {
		b.Fatal(err)
	}
	frame, err := audio.NewFrame[int16](timing.UnknownPTS(), samples, planeHandle)
	if err != nil {
		planeHandle.Release()
		b.Fatal(err)
	}
	defer frame.Release()
	encodedView := encodedHandle.Bytes()
	decoded := make([]byte, len(encoded))
	encodedOutput := make([]byte, len(encoded))
	destinations := [2][]byte{decoded}
	scratch := make([]byte, pcmBlockBytes)
	encodeScratch := make([]int16, pcmBlockBytes/2)

	b.Run("decode/direct-slice", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(samples, "samples/op")
		for range b.N {
			for index := range samples {
				value := binary.LittleEndian.Uint16(encoded[index*2:])
				binary.NativeEndian.PutUint16(decoded[index*2:], value)
			}
		}
		benchmarkPCMSum = uint64(decoded[len(decoded)-1])
	})
	b.Run("decode/immutable-view", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(samples, "samples/op")
		for range b.N {
			if err := decodePCM(encodedView, scratch, destinations, 1, samples, 0, true); err != nil {
				b.Fatal(err)
			}
		}
		benchmarkPCMSum = uint64(decoded[len(decoded)-1])
	})
	b.Run("encode/direct-slice", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(samples, "samples/op")
		for range b.N {
			for index, value := range directSamples {
				binary.LittleEndian.PutUint16(encodedOutput[index*2:], uint16(value))
			}
		}
		benchmarkPCMSum = uint64(encodedOutput[len(encodedOutput)-1])
	})
	b.Run("encode/immutable-view", func(b *testing.B) {
		b.ReportAllocs()
		b.ReportMetric(samples, "samples/op")
		for range b.N {
			if err := encodePCM(frame, encodeScratch, encodedOutput, 1, 0, true); err != nil {
				b.Fatal(err)
			}
		}
		benchmarkPCMSum = uint64(encodedOutput[len(encodedOutput)-1])
	})
}
