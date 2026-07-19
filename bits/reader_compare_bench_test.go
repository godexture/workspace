package bits

import (
	"testing"
	"time"
)

var benchBits64Sink uint64

func BenchmarkBits64Paired(b *testing.B) {
	data := make([]byte, 8192)
	for i := range data {
		data[i] = byte(i * 37)
	}
	current := &Reader{}
	generic := &Reader{}
	var currentDuration, genericDuration time.Duration
	var iterations int64
	var sum uint64
	runCurrent := func() {
		current.Init(data, 0, int32(len(data))*8)
		for current.Remaining() >= 9 {
			sum += current.Bits64(9)
		}
	}
	runGeneric := func() {
		generic.Init(data, 0, int32(len(data))*8)
		for generic.Remaining() >= 9 {
			sum += generic.bits64Wide(9)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		if iterations&1 == 0 {
			start := time.Now()
			runCurrent()
			currentDuration += time.Since(start)
			start = time.Now()
			runGeneric()
			genericDuration += time.Since(start)
		} else {
			start := time.Now()
			runGeneric()
			genericDuration += time.Since(start)
			start = time.Now()
			runCurrent()
			currentDuration += time.Since(start)
		}
		iterations++
	}
	benchBits64Sink = sum
	if iterations > 0 {
		b.ReportMetric(float64(currentDuration.Nanoseconds())/float64(iterations), "current-ns/op")
		b.ReportMetric(float64(genericDuration.Nanoseconds())/float64(iterations), "generic-ns/op")
	}
}
