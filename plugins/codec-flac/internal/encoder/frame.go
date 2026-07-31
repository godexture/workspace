package encoder

import (
	"fmt"

	"github.com/godexture/godec/plugins/codec-flac/internal/config"
	"github.com/godexture/godec/plugins/codec-flac/internal/flac"
	"github.com/godexture/godec/plugins/format-flac/frame"
	"github.com/godexture/godec/sdk/bits"
	"github.com/godexture/godec/sdk/hash"
)

func EncodeFrame(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, options config.EncoderConfig) ([]byte, error) {
	var writer bits.Writer
	windows := newWindowSet(options.Apodizations)
	return encodeFrameWithWriter(samples, sampleRate, bitsPerSample, frameNumber, options, false, &windows, &writer)
}

func encodeFrameWithWriter(samples [][]int64, sampleRate, bitsPerSample int, frameNumber uint64, options config.EncoderConfig, samplesValidated bool, windows *windowSet, w *bits.Writer) ([]byte, error) {
	if err := validateFrame(samples, sampleRate, bitsPerSample, options, samplesValidated); err != nil {
		return nil, err
	}
	analysis, err := analyzeFrame(samples, bitsPerSample, options, windows)
	if err != nil {
		return nil, err
	}
	defer analysis.release()
	return writeAnalyzedFrame(w, analysis, len(samples[0]), sampleRate, bitsPerSample, frameNumber, options.BlockSplitDepth > 0, options.StreamableSubset)
}

type frameAnalysis struct {
	assignment uint8
	channels   [][]int64
	candidates []subframeCandidate
	scratch    [][]int64
	costBits   uint64
}

const frameOverheadBits = 80

func (a *frameAnalysis) release() {
	if a == nil {
		return
	}
	releaseSubframeCandidates(a.candidates)
	releaseResidualBuffers(a.scratch)
	a.channels, a.candidates, a.scratch = nil, nil, nil
}

func analyzeFrame(samples [][]int64, bitsPerSample int, options config.EncoderConfig, windows *windowSet) (*frameAnalysis, error) {
	assignment, channels, candidates, scratch, err := chooseChannelAssignment(samples, bitsPerSample, options, windows)
	if err != nil {
		return nil, err
	}
	analysis := &frameAnalysis{assignment: assignment, channels: channels, candidates: candidates, scratch: scratch, costBits: frameOverheadBits}
	for _, candidate := range candidates {
		analysis.costBits += candidate.costBits
	}
	return analysis, nil
}

func writeAnalyzedFrame(w *bits.Writer, analysis *frameAnalysis, blockSize, sampleRate, bitsPerSample int, number uint64, variable, streamableSubset bool) ([]byte, error) {
	w.Init()
	w.Grow(int((analysis.costBits+7)/8) + 16)
	header := &frame.Header{
		BlockSize: blockSize, SampleRate: sampleRate,
		ChannelAssignment: assignmentForChannels(analysis.assignment, len(analysis.channels)),
		BitsPerSample:     bitsPerSample, Number: number, BlockingStrategy: variable,
	}
	if err := frame.EncodeHeader(w, header, streamableSubset); err != nil {
		return nil, err
	}
	for ch := range analysis.channels {
		channelBits := bitsPerSample + sideChannelOffset(analysis.assignment, ch)
		if err := EncodeSubframeCandidate(w, analysis.channels[ch], channelBits, analysis.candidates[ch]); err != nil {
			return nil, fmt.Errorf("encode FLAC subframe %d: %w", ch, err)
		}
	}
	w.PadToByte()
	crc := hash.CRC16(w.Bytes())
	w.Byte(byte(crc >> 8))
	w.Byte(byte(crc))
	return w.Bytes(), nil
}

func validateFrame(samples [][]int64, sampleRate, bitsPerSample int, options config.EncoderConfig, samplesValidated bool) error {
	if bitsPerSample < 4 || bitsPerSample > 32 {
		return fmt.Errorf("unsupported FLAC bit depth: %d", bitsPerSample)
	}
	if sampleRate < 1 || sampleRate > 1048575 {
		return fmt.Errorf("invalid FLAC sample rate: %d", sampleRate)
	}
	if options.MaxRicePartitionOrder < 0 || options.MaxRicePartitionOrder > 15 || options.StreamableSubset && options.MaxRicePartitionOrder > 8 {
		return fmt.Errorf("invalid FLAC Rice partition order: %d", options.MaxRicePartitionOrder)
	}
	if options.LPCPrecision != 0 && (options.LPCPrecision < 4 || options.LPCPrecision > 15) {
		return fmt.Errorf("invalid FLAC LPC precision: %d", options.LPCPrecision)
	}
	if options.StereoMode != "" && options.StereoMode != config.StereoIndependent && options.StereoMode != config.StereoAdaptive && options.StereoMode != config.StereoExhaustive {
		return fmt.Errorf("invalid FLAC stereo mode: %q", options.StereoMode)
	}
	if len(samples) == 0 || len(samples) > 8 {
		return fmt.Errorf("unsupported FLAC channel count: %d", len(samples))
	}
	blockSize := len(samples[0])
	if blockSize == 0 {
		return fmt.Errorf("FLAC frame has no samples")
	}
	if blockSize > 65535 {
		return fmt.Errorf("FLAC frame block size exceeds 65535: %d", blockSize)
	}
	if options.StreamableSubset {
		if maxBlockSize := streamableMaxBlockSize(sampleRate); blockSize > maxBlockSize {
			return fmt.Errorf("FLAC streamable-subset block size %d exceeds %d at %d Hz", blockSize, maxBlockSize, sampleRate)
		}
		if sampleRate <= 48000 && options.MaxLPCOrder > streamableMaxLPCOrder {
			options.MaxLPCOrder = streamableMaxLPCOrder
		}
	}
	for ch := range samples {
		if len(samples[ch]) != blockSize {
			return fmt.Errorf("FLAC channel %d has mismatched block size", ch)
		}
		if !samplesValidated {
			if err := flac.ValidateSampleRange(samples[ch], bitsPerSample); err != nil {
				return err
			}
		}
	}

	return nil
}

func streamableMaxBlockSize(sampleRate int) int {
	if sampleRate <= 48000 {
		return 4608
	}
	return 16384
}
