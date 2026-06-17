package registry

import "fmt"

func (b Bundle) Register(config Configuration, manifest Manifest) error {
	switch m := manifest.(type) {
	case MuxerManifest:
		return b.Muxers.Register(config, m)

	case DemuxerManifest:
		return b.Demuxers.Register(config, m)

	case EncoderManifest:
		return b.Encoders.Register(config, m)

	case DecoderManifest:
		return b.Decoders.Register(config, m)

	case FilterManifest:
		return b.Filters.Register(config, m)

	default:
		return fmt.Errorf("unsupported manifest type: %T", manifest)
	}
}
