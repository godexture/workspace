package registry

type DemuxerRegistry = Registry[DemuxerManifest]
type MuxerRegistry = Registry[MuxerManifest]
type EncoderRegistry = Registry[EncoderManifest]
type DecoderRegistry = Registry[DecoderManifest]
type FilterRegistry = Registry[FilterManifest]

type Bundle struct {
	Demuxers *DemuxerRegistry
	Muxers   *MuxerRegistry
	Encoders *EncoderRegistry
	Decoders *DecoderRegistry
	Filters  *FilterRegistry
}
