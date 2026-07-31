package cli

import (
	"runtime"
	"sync"
	"time"
)

type runtimeMetrics struct {
	HeapAlloc      uint64
	PeakHeapAlloc  uint64
	HeapInuse      uint64
	PeakHeapInuse  uint64
	System         uint64
	PeakSystem     uint64
	TotalAllocated uint64
	Mallocs        uint64
	Frees          uint64
	GCs            uint32
	GCPause        time.Duration
	PeakGoroutines int
}

type runtimeSampler struct {
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	result runtimeMetrics
}

func startRuntimeSampler(interval time.Duration) *runtimeSampler {
	sampler := &runtimeSampler{stop: make(chan struct{}), done: make(chan struct{})}
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	go sampler.run(interval, baseline)
	return sampler
}

func (sampler *runtimeSampler) run(interval time.Duration, baseline runtime.MemStats) {
	defer close(sampler.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	peakHeapAlloc := baseline.HeapAlloc
	peakHeapInuse := baseline.HeapInuse
	peakSystem := baseline.Sys
	peakGoroutines := runtime.NumGoroutine()
	var current runtime.MemStats
	for {
		select {
		case <-ticker.C:
			runtime.ReadMemStats(&current)
			peakHeapAlloc = max(peakHeapAlloc, current.HeapAlloc)
			peakHeapInuse = max(peakHeapInuse, current.HeapInuse)
			peakSystem = max(peakSystem, current.Sys)
			peakGoroutines = max(peakGoroutines, runtime.NumGoroutine())
		case <-sampler.stop:
			runtime.ReadMemStats(&current)
			peakHeapAlloc = max(peakHeapAlloc, current.HeapAlloc)
			peakHeapInuse = max(peakHeapInuse, current.HeapInuse)
			peakSystem = max(peakSystem, current.Sys)
			peakGoroutines = max(peakGoroutines, runtime.NumGoroutine())
			sampler.result = runtimeMetrics{
				HeapAlloc:      current.HeapAlloc,
				PeakHeapAlloc:  peakHeapAlloc,
				HeapInuse:      current.HeapInuse,
				PeakHeapInuse:  peakHeapInuse,
				System:         current.Sys,
				PeakSystem:     peakSystem,
				TotalAllocated: current.TotalAlloc - baseline.TotalAlloc,
				Mallocs:        current.Mallocs - baseline.Mallocs,
				Frees:          current.Frees - baseline.Frees,
				GCs:            current.NumGC - baseline.NumGC,
				GCPause:        time.Duration(current.PauseTotalNs - baseline.PauseTotalNs),
				PeakGoroutines: peakGoroutines,
			}
			return
		}
	}
}

func (sampler *runtimeSampler) Stop() runtimeMetrics {
	sampler.once.Do(func() { close(sampler.stop) })
	<-sampler.done
	return sampler.result
}
