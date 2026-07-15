package media

type MediaAttributes struct {
	Codec           CodecID
	CodecParameters CodecParameters

	Video VideoAttributes
	Audio AudioAttributes
}

type VideoAttributes struct {
	/* NOT IMPLEMENTED */
}

type AudioAttributes struct {
	SampleRate    int
	Format        SampleFormat
	BitsPerSample int
	ChannelLayout ChannelLayout
}

func (a AudioAttributes) ChannelCount() int {
	return a.ChannelLayout.ChannelCount()
}

// CodecParameters is an opaque codec configuration payload.
//
// Consumers must only interpret Data when they recognize Schema. The type is
// deliberately generic so codec and container plugins can evolve without
// adding codec-specific fields to core media types.
type CodecParameters struct {
	Schema string
	Data   []byte
}

// Clone returns an independent copy of the parameter payload.
func (p CodecParameters) Clone() CodecParameters {
	if p.Data == nil {
		return p
	}
	p.Data = append([]byte(nil), p.Data...)
	return p
}
