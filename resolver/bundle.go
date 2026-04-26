package resolver

type Bundle struct {
	Muxer   *DefaultMuxerResolver
	Demuxer *DefaultDemuxerResolver
	Encoder *DefaultEncoderResolver
	Decoder *DefaultDecoderResolver
}
