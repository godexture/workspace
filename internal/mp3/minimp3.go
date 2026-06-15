package mp3

import "errors"

var (
	ErrInvalidSideInfo       = errors.New("invalid side info")
	ErrBitstreamUnderflow    = errors.New("bitstream buffer underflow")
	ErrInsufficientReservoir = errors.New("insufficient data in reservoir")
)

const (
	MaxBitreservoirBytes     = 511
	MaxGranuleBufferSize     = SamplesPerGranuleLayer3 * 2
	MaxScalefactorBands      = 39
	NumSubbands              = 32
	SamplesPerSubbandLayer3  = 18
	SamplesPerSubbandLayer12 = 12
	MaxChannels              = 2
	QMFHistoryBlocks         = 15
)

// Mp3Dec is the Go decoder state.
type Mp3Dec struct {
	MdctOverlap     [MaxChannels][(SamplesPerSubbandLayer3 / 2) * NumSubbands]float32
	QmfState        [QMFHistoryBlocks * MaxChannels * NumSubbands]float32
	Reserv          int
	FreeFormatBytes int
	Header          Header
	ReservBuf       [MaxBitreservoirBytes]byte
	workspace       decoderWorkspace
}

// Mp3DecFrameInfo contains parsed MPEG frame header information.
type Mp3DecFrameInfo struct {
	FrameBytes  int
	FrameOffset int
	Channels    int
	Hz          int
	Layer       int
	BitrateKbps int
}

// Init initializes the decoder.
func (dec *Mp3Dec) Init() {
	*dec = Mp3Dec{}
}

// DecodeFrame decodes one MP3 frame to float32 samples.
// pcm slice must be pre-allocated to hold up to SamplesPerFrameLayer23 * 2 samples.
// Returns the number of samples decoded *per channel*, the frame info, and any error encountered.
func (dec *Mp3Dec) DecodeFrame(mp3 []byte, pcm []float32) (int, Mp3DecFrameInfo, error) {
	mp3Bytes := len(mp3)
	frameSize := 0
	i := 0

	if mp3Bytes > 4 && dec.Header[0] == 0xff {
		nextHdr, err := ParseHeader(mp3)
		if err == nil && dec.Header.Compare(nextHdr) {
			frameSize = nextHdr.FrameBytes(dec.FreeFormatBytes) + nextHdr.Padding()
			if frameSize != mp3Bytes {
				if frameSize+4 > mp3Bytes {
					frameSize = 0
				} else {
					nextHdr2, err2 := ParseHeader(mp3[frameSize:])
					if err2 != nil || !nextHdr.Compare(nextHdr2) {
						frameSize = 0
					}
				}
			}
		}
	}

	if frameSize == 0 {
		dec.Init()
		var found bool
		i, frameSize, dec.FreeFormatBytes, found = FindFrame(mp3, dec.FreeFormatBytes)
		if !found || i+frameSize > mp3Bytes {
			return 0, Mp3DecFrameInfo{FrameBytes: i}, nil
		}
	}

	hdr, err := ParseHeader(mp3[i : i+4])
	if err != nil {
		return 0, Mp3DecFrameInfo{}, nil
	}
	dec.Header = hdr

	info := Mp3DecFrameInfo{
		FrameBytes:  i + frameSize,
		FrameOffset: i,
		Channels:    MaxChannels,
		Hz:          hdr.SampleRateHz(),
		Layer:       4 - hdr.Layer(),
		BitrateKbps: hdr.BitrateKbps(),
	}
	if hdr.IsMono() {
		info.Channels = 1
	}

	if pcm == nil {
		return hdr.FrameSamples(), info, nil
	}

	var bsFrame bitReader
	bsFrame.buf = mp3[i+4:]
	bsFrame.pos = 0
	bsFrame.limit = int32((frameSize - 4) * 8)

	if hdr.IsCrc() {
		bsFrame.getBits(16)
	}

	if info.Layer == 3 {
		err = dec.decodeLayer3(info, &bsFrame, &dec.workspace, pcm, hdr)
	} else {
		err = dec.decodeLayer12(info, &bsFrame, &dec.workspace, pcm, hdr)
	}

	if err != nil {
		return 0, info, err
	}
	return dec.Header.FrameSamples(), info, nil
}

func (dec *Mp3Dec) decodeLayer3(info Mp3DecFrameInfo, bsFrame *bitReader, scratch *decoderWorkspace, pcm []float32, hdr Header) error {
	mainDataBegin := readSideInfoL3(bsFrame, scratch.gr_info[:], hdr)
	if mainDataBegin < 0 || bsFrame.pos > bsFrame.limit {
		dec.Init()
		if mainDataBegin < 0 {
			return ErrInvalidSideInfo
		}
		return ErrBitstreamUnderflow
	}
	if err := restoreReservoirL3(dec, bsFrame, scratch, mainDataBegin); err != nil {
		saveReservoirL3(dec, scratch)
		return err
	}
	pcmOffset := 0
	igrLimit := 1
	if hdr.TestMpeg1() {
		igrLimit = 2
	}
	for igr := 0; igr < igrLimit; igr++ {
		scratch.grbuf = [MaxGranuleBufferSize]float32{}
		decodeL3(dec, scratch, scratch.gr_info[:], igr*info.Channels, info.Channels)
		synthGranule(dec.QmfState[:], scratch.grbuf[:], SamplesPerSubbandLayer3, info.Channels, pcm[pcmOffset:], scratch.syn[:])
		pcmOffset += SamplesPerGranuleLayer3 * info.Channels
	}
	saveReservoirL3(dec, scratch)
	return nil
}

func (dec *Mp3Dec) decodeLayer12(info Mp3DecFrameInfo, bsFrame *bitReader, scratch *decoderWorkspace, pcm []float32, hdr Header) error {
	var sci l12ScaleInfo
	readScaleInfoL12(hdr, bsFrame, &sci)

	scratch.grbuf = [MaxGranuleBufferSize]float32{}

	iVal := 0
	pcmOffset := 0
	grbufFlat := scratch.grbuf[:]
	for igr := 0; igr < 3; igr++ {
		deqVal := dequantizeGranuleL12(grbufFlat[iVal:], bsFrame, &sci, info.Layer|1)
		iVal += deqVal
		if iVal == SamplesPerSubbandLayer12 {
			iVal = 0
			applyScf384L12(&sci, sci.Scf[igr:], grbufFlat)
			synthGranule(dec.QmfState[:], grbufFlat, SamplesPerSubbandLayer12, info.Channels, pcm[pcmOffset:], scratch.syn[:])
			scratch.grbuf = [MaxGranuleBufferSize]float32{}
			pcmOffset += SamplesPerFrameLayer1 * info.Channels
		}
		if bsFrame.pos > bsFrame.limit {
			dec.Init()
			return ErrBitstreamUnderflow
		}
	}
	return nil
}
