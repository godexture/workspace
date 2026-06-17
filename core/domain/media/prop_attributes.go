package media

type MediaAttributes struct {
	Codec CodecID

	Video VideoAttributes
	Audio AudioAttributes
}

type VideoAttributes struct {
	/* NOT IMPLEMENTED */
}

type AudioAttributes struct {
	SampleRate    int
	Format        SampleFormat
	ChannelLayout ChannelLayout
}

func (a AudioAttributes) ChannelCount() int {
	return a.ChannelLayout.ChannelCount()
}
