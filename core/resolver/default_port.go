package resolver

import (
	"errors"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
)

const (
	DefaultPortBonus = 1 << 48
)

func ResolveDefaultAudioPort[T any](ports map[string]node.OutPort[T]) (*node.OutPort[T], error) {
	var bestPort *node.OutPort[T]
	var maxScore int64 = -1

	for _, port := range ports {
		info := port.StreamInfo()
		if info.Type != media.MediaAudio {
			continue
		}

		var score int64 = 0

		if info.IsDefault {
			score += DefaultPortBonus
		}

		qualityScore := int64(info.Audio.ChannelCount()) * int64(info.Audio.SampleRate)
		score += qualityScore

		if score > maxScore {
			maxScore = score
			bestPort = &port
		} else if score == maxScore && bestPort != nil {
			port, ok := ports[bestPort.ID()]
			if !ok {
				continue
			}

			bestPortInfo := port.StreamInfo()

			if info.Index < bestPortInfo.Index {
				bestPort = &port
			}
		}
	}

	if bestPort == nil {
		return nil, errors.New("no audio port found")
	}

	return bestPort, nil
}
