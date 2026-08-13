package solve

import (
	"math"
	"time"

	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
)

type rank struct {
	preservation int
	copy         int
	loss         int
	contract     int
	resource1    uint64
	resource2    uint64
	resource3    uint64
	steps        int
	tie          string
}

func (r rank) less(other rank) bool {
	left := [...]uint64{uint64(r.preservation), uint64(r.copy), uint64(r.loss), uint64(r.contract), r.resource1, r.resource2, r.resource3, uint64(r.steps)}
	right := [...]uint64{uint64(other.preservation), uint64(other.copy), uint64(other.loss), uint64(other.contract), other.resource1, other.resource2, other.resource3, uint64(other.steps)}
	for index := range left {
		if left[index] != right[index] {
			return left[index] < right[index]
		}
	}
	return r.tie < other.tie
}

func (r rank) add(result candidateResult, policy job.Policy) rank {
	for _, effect := range result.compilation.Effects() {
		switch effect.Kind {
		case plugin.RepresentationEffect:
			r.copy = saturatingInt(r.copy, 1)
		case plugin.CompressionEffect:
			r.copy = saturatingInt(r.copy, 2)
		}
		r.loss = saturatingInt(r.loss, int(effect.Loss))
	}
	contract := result.bridge.component.Contract()
	if contract.Accuracy != plugin.ExactContract {
		r.contract = saturatingInt(r.contract, 1)
	}
	if contract.Repeatability != plugin.RepeatableContract {
		r.contract = saturatingInt(r.contract, 1)
	}
	estimate := result.compilation.Estimate()
	request := result.compilation.Resources()
	cpu := uint64(estimate.CPU)
	latency := durationValue(estimate.Latency)
	memory := saturatingAdd(uint64(estimate.Memory), uint64(request.Memory))
	switch policy.Goal {
	case job.LatencyGoal:
		r.resource1 = saturatingAdd(r.resource1, latency)
		r.resource2 = saturatingAdd(r.resource2, cpu)
		r.resource3 = saturatingAdd(r.resource3, memory)
	case job.MemoryGoal:
		r.resource1 = saturatingAdd(r.resource1, memory)
		r.resource2 = saturatingAdd(r.resource2, cpu)
		r.resource3 = saturatingAdd(r.resource3, latency)
	default:
		r.resource1 = saturatingAdd(r.resource1, cpu)
		r.resource2 = saturatingAdd(r.resource2, latency)
		r.resource3 = saturatingAdd(r.resource3, memory)
	}
	r.steps = saturatingInt(r.steps, 1)
	r.tie += "\x00" + result.bridge.component.Identity().String() + "@" + result.config.Fingerprint().String()
	return r
}

func durationValue(value time.Duration) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func saturatingInt(value, add int) int {
	if add > 0 && value > math.MaxInt-add {
		return math.MaxInt
	}
	return value + add
}

func saturatingAdd(values ...uint64) uint64 {
	var result uint64
	for _, value := range values {
		if math.MaxUint64-result < value {
			return math.MaxUint64
		}
		result += value
	}
	return result
}
