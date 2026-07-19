package resolver

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/registry"
)

type DefaultDemuxerResolver struct {
	registry    *registry.Registry[registry.DemuxerManifest]
	baseOptions *ResolveOptions
}

func NewDefaultDemuxerResolver(reg *registry.Registry[registry.DemuxerManifest], opts ...Option) *DefaultDemuxerResolver {
	return &DefaultDemuxerResolver{
		registry:    reg,
		baseOptions: parseOptions(nil, opts...),
	}
}

func (r *DefaultDemuxerResolver) ResolveDemuxer(stream io.ReadSeeker, opts ...Option) (registry.DemuxerManifest, error) {
	options := parseOptions(r.baseOptions, opts...)

	var bestManifest registry.DemuxerManifest
	var maxScore manifest.ProbeScore = 0
	var maxPriority Priority = -1

	for manifest := range r.registry.Enumerate() {
		if manifest.Probe == nil {
			continue
		}

		if _, err := stream.Seek(0, io.SeekStart); err != nil {
			return bestManifest, fmt.Errorf("seek input before probing %s: %w", manifest.Name, err)
		}
		score := manifest.Probe(stream)
		priority := options.priority(manifest.ID())

		if score > maxScore {
			maxScore = score
			maxPriority = priority
			bestManifest = manifest
		} else if score == maxScore && maxScore > 0 && priority > maxPriority {
			maxPriority = priority
			bestManifest = manifest
		}
	}

	if _, err := stream.Seek(0, io.SeekStart); err != nil {
		return bestManifest, fmt.Errorf("rewind input after probing: %w", err)
	}
	if maxScore == 0 {
		return bestManifest, errors.New("unsupported format")
	}

	return bestManifest, nil
}
