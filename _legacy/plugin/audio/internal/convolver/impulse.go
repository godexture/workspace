package convolver

import (
	"fmt"
	"sync"

	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/sdk/dsp"
	"github.com/godexture/godec/sdk/dsp/fft"
)

func (e *Engine) buildImpulse(impulse [][]float32, rate int) error {
	ir := impulse
	if len(ir) == 0 {
		return fmt.Errorf("convolution impulse response has no channels")
	}
	for ch, samples := range ir {
		if len(samples) == 0 {
			return fmt.Errorf("convolution impulse response channel %d is empty", ch)
		}
	}
	if e.cfg.Normalize {
		ir = dsp.ClampL1(ir)
	}
	partitions := make([][]partition, len(ir))
	for ch, samples := range ir {
		parts, err := buildPartitions(e.plan, e.hop, samples, e.pool)
		if err != nil {
			return err
		}
		partitions[ch] = parts
	}
	e.partitions = partitions
	e.tailHops = len(partitions[0]) - 1
	e.irRate = rate
	return nil
}

func (e *Engine) partitionsFor(channel int) []partition {
	if len(e.partitions) == 1 {
		return e.partitions[0]
	}
	return e.partitions[channel]
}

// partitionError collects the first error from concurrent partition tasks.
type partitionError struct {
	sync.Mutex
	value error
}

// partitionTask forward-transforms one partition on a shared WorkerPool
// worker. It implements registry.Task directly so it can be submitted
// without an extra closure allocation on top of the task struct itself.
type partitionTask struct {
	plan    *fft.RealPlan
	hop     int
	samples []float32
	index   int
	result  []partition
	group   *sync.WaitGroup
	errs    *partitionError
}

func (t *partitionTask) Run() {
	defer t.group.Done()
	part, err := transformPartition(t.plan.Clone(), t.hop, t.samples, t.index)
	if err != nil {
		t.errs.Lock()
		if t.errs.value == nil {
			t.errs.value = err
		}
		t.errs.Unlock()
		return
	}
	t.result[t.index] = part
}

// buildPartitions splits samples into hop-length segments (the last one
// zero-padded), and forward-transforms each into a spectrum ready for the
// frequency-domain delay line.
func buildPartitions(plan *fft.RealPlan, hop int, samples []float32, pool *registry.WorkerPool) ([]partition, error) {
	count := (len(samples) + hop - 1) / hop
	result := make([]partition, count)
	if pool == nil || count == 1 {
		for index := range result {
			part, err := transformPartition(plan, hop, samples, index)
			if err != nil {
				return nil, err
			}
			result[index] = part
		}
		return result, nil
	}

	var group sync.WaitGroup
	errs := &partitionError{}
	for i := range result {
		group.Add(1)
		pool.Submit(&partitionTask{
			plan:    plan,
			hop:     hop,
			samples: samples,
			index:   i,
			result:  result,
			group:   &group,
			errs:    errs,
		})
	}
	group.Wait()
	if errs.value != nil {
		return nil, errs.value
	}
	return result, nil
}

func transformPartition(plan *fft.RealPlan, hop int, samples []float32, index int) (partition, error) {
	windowed := make([]float32, 2*hop)
	start := index * hop
	end := min(start+hop, len(samples))
	copy(windowed[:hop], samples[start:end])
	spectrum := make([]complex64, plan.Bins())
	if err := plan.Forward(spectrum, windowed); err != nil {
		return partition{}, err
	}
	return partition{spectrum: spectrum}, nil
}
