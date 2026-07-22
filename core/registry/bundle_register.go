package registry

import "fmt"

func (b Bundle) Register(manifest Manifest) error {
	switch m := manifest.(type) {
	case MuxerManifest:
		return b.Muxers.Register(m)

	case DemuxerManifest:
		return b.Demuxers.Register(m)

	case EncoderManifest:
		return b.Encoders.Register(m)

	case DecoderManifest:
		return b.Decoders.Register(m)

	case FilterManifest:
		return b.Filters.Register(m)

	default:
		return fmt.Errorf("unsupported manifest type: %T", manifest)
	}
}
