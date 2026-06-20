package registry

type MuxerRegistry = Registry[MuxerManifest]
type DemuxerRegistry = Registry[DemuxerManifest]
type EncoderRegistry = Registry[EncoderManifest]
type DecoderRegistry = Registry[DecoderManifest]
type FilterRegistry = Registry[FilterManifest]

type Bundle struct {
	Muxers   *MuxerRegistry
	Demuxers *DemuxerRegistry
	Encoders *EncoderRegistry
	Decoders *DecoderRegistry
	Filters  *FilterRegistry
}
