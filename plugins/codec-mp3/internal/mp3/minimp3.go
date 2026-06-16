package mp3

import "errors"

var (
	ErrInvalidSideInfo       = errors.New("invalid side info")
	ErrBitStreamUnderflow    = errors.New("bitStream buffer underflow")
	ErrInsufficientReservoir = errors.New("insufficient data in reservoir")
)

const (
	MaxBitReservoirBytes     = 511
	MaxGranuleBufferSize     = SamplesPerGranuleLayer3 * 2
	MaxScaleFactorBands      = 39
	NumSubBands              = 32
	SamplesPerSubBandLayer3  = 18
	SamplesPerSubBandLayer12 = 12
	MaxChannels              = 2
	QMFHistoryBlocks         = 15
)

// Decoder is the Go decoder state.
type Decoder struct {
	MdctOverlap                 [MaxChannels][(SamplesPerSubBandLayer3 / 2) * NumSubBands]float32
	QuadratureMirrorFilterState [QMFHistoryBlocks * MaxChannels * NumSubBands]float32
	BitReservoirBytes           int
	FreeFormatBytes             int
	Header                      Header
	ReservoirBuffer             [MaxBitReservoirBytes]byte
	workspace                   decoderWorkspace
}

// DecoderFrameInfo contains parsed MPEG frame header information.
type DecoderFrameInfo struct {
	FrameBytes               int
	FrameOffset              int
	Channels                 int
	SampleRateHertz          int
	MpegLayer                int
	BitRateKilobitsPerSecond int
}

// Init initializes the decoder.
func (d *Decoder) Init() {
	*d = Decoder{}
} // DecodeFrame decodes one MP3 frame to float32 samples.
// pcmSamples slice must be pre-allocated to hold up to SamplesPerFrameLayer23 * 2 samples.
// Returns the number of samples decoded *per channel*, the frame info, and any error encountered.
func (d *Decoder) DecodeFrame(mp3Data []byte, pcmSamples []float32) (int, DecoderFrameInfo, error) {
	mp3DataLength := len(mp3Data)
	frameSize := 0
	byteIndex := 0

	if mp3DataLength > 4 && d.Header[0] == 0xff {
		nextHeader, err := ParseHeader(mp3Data)
		if err == nil && d.Header.Compare(nextHeader) {
			frameSize = nextHeader.FrameBytes(d.FreeFormatBytes) + nextHeader.Padding()
			if frameSize != mp3DataLength {
				if frameSize+4 > mp3DataLength {
					frameSize = 0
				} else {
					nextHeader2, err2 := ParseHeader(mp3Data[frameSize:])
					if err2 != nil || !nextHeader.Compare(nextHeader2) {
						frameSize = 0
					}
				}
			}
		}
	}

	if frameSize == 0 {
		d.Init()
		var frameFound bool
		byteIndex, frameSize, d.FreeFormatBytes, frameFound = FindFrame(mp3Data, d.FreeFormatBytes)
		if !frameFound || byteIndex+frameSize > mp3DataLength {
			return 0, DecoderFrameInfo{FrameBytes: byteIndex}, nil
		}
	}

	header, err := ParseHeader(mp3Data[byteIndex : byteIndex+4])
	if err != nil {
		return 0, DecoderFrameInfo{}, nil
	}
	d.Header = header

	frameInfo := DecoderFrameInfo{
		FrameBytes:               byteIndex + frameSize,
		FrameOffset:              byteIndex,
		Channels:                 MaxChannels,
		SampleRateHertz:          header.SampleRateHz(),
		MpegLayer:                4 - header.Layer(),
		BitRateKilobitsPerSecond: header.BitrateKbps(),
	}
	if header.IsMono() {
		frameInfo.Channels = 1
	}

	if pcmSamples == nil {
		return header.FrameSamples(), frameInfo, nil
	}

	var bitStreamFrame BitReader
	bitStreamFrame.buffer = mp3Data[byteIndex+4:]
	bitStreamFrame.position = 0
	bitStreamFrame.limit = int32((frameSize - 4) * 8)

	if header.IsCyclicRedundancyCheck() {
		bitStreamFrame.getBits(16)
	}

	if frameInfo.MpegLayer == 3 {
		err = d.decodeLayer3(frameInfo, &bitStreamFrame, &d.workspace, pcmSamples, header)
	} else {
		err = d.decodeLayer12(frameInfo, &bitStreamFrame, &d.workspace, pcmSamples, header)
	}

	if err != nil {
		return 0, frameInfo, err
	}
	return d.Header.FrameSamples(), frameInfo, nil
}

func (d *Decoder) decodeLayer3(frameInfo DecoderFrameInfo, bitStreamFrame *BitReader, workspace *decoderWorkspace, pcmSamples []float32, header Header) error {
	mainDataBegin := readSideInfoLayer3(bitStreamFrame, workspace.granuleInfo[:], header)
	if mainDataBegin < 0 || bitStreamFrame.position > bitStreamFrame.limit {
		d.Init()
		if mainDataBegin < 0 {
			return ErrInvalidSideInfo
		}
		return ErrBitStreamUnderflow
	}
	if err := restoreReservoirLayer3(d, bitStreamFrame, workspace, mainDataBegin); err != nil {
		saveReservoirLayer3(d, workspace)
		return err
	}
	pcmOffset := 0
	granuleLimit := 1
	if header.IsMPEG1() {
		granuleLimit = 2
	}
	for granuleIndex := 0; granuleIndex < granuleLimit; granuleIndex++ {
		workspace.granule = [MaxGranuleBufferSize]float32{}
		decodeLayer3(d, workspace, workspace.granuleInfo[:], granuleIndex*frameInfo.Channels, frameInfo.Channels)
		SynthesizeGranule(d.QuadratureMirrorFilterState[:], workspace.granule[:], SamplesPerSubBandLayer3, frameInfo.Channels, pcmSamples[pcmOffset:], workspace.synthesisWorkspace[:])
		pcmOffset += SamplesPerGranuleLayer3 * frameInfo.Channels
	}
	saveReservoirLayer3(d, workspace)
	return nil
}

func (d *Decoder) decodeLayer12(frameInfo DecoderFrameInfo, bitStreamFrame *BitReader, workspace *decoderWorkspace, pcmSamples []float32, header Header) error {
	var scaleFactorInfo Layer12ScaleFactorInfo
	readScaleFactorInfoLayer12(header, bitStreamFrame, &scaleFactorInfo)

	workspace.granule = [MaxGranuleBufferSize]float32{}

	iVal := 0
	pcmOffset := 0
	granuleFlat := workspace.granule[:]
	for granuleIndex := 0; granuleIndex < 3; granuleIndex++ {
		dequantizedSamplesCount := dequantizeGranuleLayer12(granuleFlat[iVal:], bitStreamFrame, &scaleFactorInfo, frameInfo.MpegLayer|1)
		iVal += dequantizedSamplesCount
		if iVal == SamplesPerSubBandLayer12 {
			iVal = 0
			applyScaleFactors384Layer12(&scaleFactorInfo, scaleFactorInfo.ScaleFactors[granuleIndex:], granuleFlat)
			SynthesizeGranule(d.QuadratureMirrorFilterState[:], granuleFlat, SamplesPerSubBandLayer12, frameInfo.Channels, pcmSamples[pcmOffset:], workspace.synthesisWorkspace[:])
			workspace.granule = [MaxGranuleBufferSize]float32{}
			pcmOffset += SamplesPerFrameLayer1 * frameInfo.Channels
		}
		if bitStreamFrame.position > bitStreamFrame.limit {
			d.Init()
			return ErrBitStreamUnderflow
		}
	}
	return nil
}
