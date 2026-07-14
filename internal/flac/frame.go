package flac

// FrameHeader represents the parsed header of a FLAC frame.
type FrameHeader struct {
	BlockSize         int
	SampleRate        int
	Channels          int
	ChannelAssignment uint8
	BitsPerSample     int
	BlockingStrategy  bool
	Number            uint64
	HeaderBytes       int
	HeaderCRC         byte
	FrameBytes        int
}

// Frame represents a fully decoded or to-be-encoded FLAC frame.
type Frame struct {
	Header  FrameHeader
	Samples [][]int64
	Bytes   int
}
