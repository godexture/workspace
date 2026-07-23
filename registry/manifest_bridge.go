package registry

import (
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
)

type ConversionCost struct {
	QualityLoss uint32
	Work        uint32
}

type ConversionCandidate struct {
	Config Configuration
	Cost   ConversionCost
}

type BridgeFunc func(
	current media.StreamInfo,
	required []manifest.Capability,
) ([]ConversionCandidate, error)

func SingleInputBridge(bridge BridgeFunc) map[string]BridgeFunc {
	if bridge == nil {
		return nil
	}
	return map[string]BridgeFunc{"in": bridge}
}
