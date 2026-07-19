package bits

import (
	"testing"
	"time"
)

var benchRiceSink uint64

func BenchmarkRice64Paired(b *testing.B) {
	const (
		count = 4096
		param = uint8(4)
	)
	var writer Writer
	state := uint32(0x12345678)
	for range count {
		state = state*1664525 + 1013904223
		quotient := uint64(state >> 30)
		remainder := uint64((state >> 8) & 0xf)
		writer.UnaryBits64(quotient<<param|remainder, param)
	}
	data := append(append([]byte(nil), writer.Bytes()...), make([]byte, 8)...)
	limit := writer.Position()

	current := &Reader{}
	split := &Reader{}
	var currentDuration, splitDuration time.Duration
	var iterations int64
	var sum uint64
	runCurrent := func() {
		current.Init(data, 0, limit)
		for range count {
			sum += current.Rice64(param)
		}
	}
	runSplit := func() {
		split.Init(data, 0, limit)
		for range count {
			sum += split.rice64Split(param)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		if iterations&1 == 0 {
			start := time.Now()
			runCurrent()
			currentDuration += time.Since(start)
			start = time.Now()
			runSplit()
			splitDuration += time.Since(start)
		} else {
			start := time.Now()
			runSplit()
			splitDuration += time.Since(start)
			start = time.Now()
			runCurrent()
			currentDuration += time.Since(start)
		}
		iterations++
	}
	benchRiceSink = sum
	if iterations > 0 {
		b.ReportMetric(float64(currentDuration.Nanoseconds())/float64(iterations), "current-ns/op")
		b.ReportMetric(float64(splitDuration.Nanoseconds())/float64(iterations), "split-ns/op")
	}
}
