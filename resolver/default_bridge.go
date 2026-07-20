package resolver

import (
	"fmt"
	"slices"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

const maxBridgeStates = 128

type DefaultBridgeResolver struct {
	registry *registry.FilterRegistry
}

func NewDefaultBridgeResolver(reg *registry.FilterRegistry) *DefaultBridgeResolver {
	return &DefaultBridgeResolver{registry: reg}
}

func (r *DefaultBridgeResolver) ResolveBridge(current media.StreamInfo, required []manifest.Capability) ([]BridgeStep, error) {
	if r == nil || r.registry == nil {
		return nil, fmt.Errorf("bridge resolver has no filter registry")
	}
	if len(required) == 0 {
		return nil, fmt.Errorf("bridge target has no capabilities")
	}
	if acceptsAny(required, current) {
		return nil, nil
	}

	filters := make([]registry.FilterManifest, 0)
	for filter := range r.registry.Enumerate() {
		if filter.Bridge != nil {
			filters = append(filters, filter)
		}
	}
	if len(filters) == 0 {
		return nil, diagnoseBridgeTarget(current, required)
	}

	pending := []bridgeState{{stream: current}}
	visited := map[string]bridgeScore{bridgeStreamSignature(current): {}}
	for len(pending) > 0 {
		next := takeBestBridgeState(&pending)
		if acceptsAny(required, next.stream) {
			return next.plan, nil
		}
		if len(visited) >= maxBridgeStates {
			return nil, fmt.Errorf("automatic conversion exceeded %d unique stream states while satisfying %s", maxBridgeStates, diagnoseBridgeTarget(current, required))
		}

		for _, filter := range filters {
			candidates, err := filter.Bridge(next.stream, required)
			if err != nil {
				return nil, fmt.Errorf("bridge filter %s: %w", filter.Name, err)
			}
			for _, candidate := range candidates {
				if candidate.Config == nil {
					return nil, fmt.Errorf("bridge filter %s returned nil configuration", filter.Name)
				}
				accepted, err := filter.Accept(next.stream, next.stream.Codec, candidate.Config)
				if err != nil {
					return nil, fmt.Errorf("resolve bridge input for %s: %w", filter.Name, err)
				}
				if !accepted {
					continue
				}
				output, err := filter.TransformStream(next.stream, next.stream.Codec, candidate.Config)
				if err != nil {
					return nil, fmt.Errorf("resolve bridge output for %s: %w", filter.Name, err)
				}
				inputKey := bridgeStreamSignature(next.stream)
				outputKey := bridgeStreamSignature(output)
				if outputKey == inputKey {
					continue
				}
				state := bridgeState{
					stream: output,
					plan: appendBridgeStep(next.plan, BridgeStep{
						Manifest: filter,
						Config:   candidate.Config,
						Input:    next.stream,
						Output:   output,
					}),
					score: addBridgeCost(next.score, candidate.Cost, filter.ID().String()),
				}
				if previous, exists := visited[outputKey]; exists && !state.score.less(previous) {
					continue
				}
				visited[outputKey] = state.score
				pending = append(pending, state)
			}
		}
	}

	return nil, diagnoseBridgeTarget(current, required)
}

type bridgeState struct {
	stream media.StreamInfo
	plan   []BridgeStep
	score  bridgeScore
}

type bridgeScore struct {
	quality uint64
	work    uint64
	steps   int
	path    string
}

func addBridgeCost(score bridgeScore, cost registry.ConversionCost, id string) bridgeScore {
	score.quality += uint64(cost.QualityLoss)
	score.work += uint64(cost.Work)
	score.steps++
	if score.path == "" {
		score.path = id
	} else {
		score.path += "," + id
	}
	return score
}

func (s bridgeScore) less(other bridgeScore) bool {
	if s.quality != other.quality {
		return s.quality < other.quality
	}
	if s.work != other.work {
		return s.work < other.work
	}
	if s.steps != other.steps {
		return s.steps < other.steps
	}
	return s.path < other.path
}

func takeBestBridgeState(states *[]bridgeState) bridgeState {
	best := 0
	for i := 1; i < len(*states); i++ {
		if (*states)[i].score.less((*states)[best].score) {
			best = i
		}
	}
	result := (*states)[best]
	*states = slices.Delete(*states, best, best+1)
	return result
}

func appendBridgeStep(plan []BridgeStep, step BridgeStep) []BridgeStep {
	result := make([]BridgeStep, len(plan)+1)
	copy(result, plan)
	result[len(plan)] = step
	return result
}

func acceptsAny(required []manifest.Capability, stream media.StreamInfo) bool {
	return slices.ContainsFunc(required, func(capability manifest.Capability) bool {
		return capability.Match(stream)
	})
}

func diagnoseBridgeTarget(stream media.StreamInfo, required []manifest.Capability) error {
	for _, capability := range required {
		if err := capability.Diagnose(stream); err != nil {
			return fmt.Errorf("no automatic conversion path: %w", err)
		}
	}
	return fmt.Errorf("no automatic conversion path")
}

func bridgeStreamSignature(stream media.StreamInfo) string {
	audio := stream.Audio
	return fmt.Sprintf("%s|%s|%d|%s|%d|%s", stream.Type, stream.Codec, audio.SampleRate, audio.Format, audio.BitsPerSample, audio.ChannelLayout)
}
