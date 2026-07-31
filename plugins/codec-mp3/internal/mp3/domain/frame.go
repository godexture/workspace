package domain

// FrameInfo contains parsed MPEG frame header information.
type FrameInfo struct {
	FrameBytes               int
	FrameOffset              int
	Channels                 int
	SampleRateHertz          int
	MpegLayer                int
	BitRateKilobitsPerSecond int
}
