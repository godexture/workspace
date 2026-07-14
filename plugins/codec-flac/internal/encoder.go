package internal

import (
	"errors"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

type Encoder struct {
	config    EncoderConfig
	configErr error

	pendingQueue []*media.Packet
	flushed      bool

	buffer        [][]int64
	buffered      int
	bufferPTS     media.Pts
	streamSet     bool
	sampleRate    int
	channels      int
	bitsPerSample int
	frameNumber   uint64
	sampleNumber  uint64
}

func NewEncoder(config EncoderConfig) *Encoder {
	config.applyDefaults()
	return &Encoder{config: config, configErr: config.validate()}
}

func (e *Encoder) SendFrame(frame *media.Frame) error {
	if e.configErr != nil {
		return e.configErr
	}
	if frame == nil || *frame == nil {
		return errors.New("flac encoder received nil frame")
	}
	if e.flushed {
		return engine.ErrEOF
	}

	af, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return errors.New("flac encoder expected *media.AudioFrame")
	}

	samples, sampleRate, bitsPerSample, err := audioFrameToSamples(af, e.config.BitsPerSample)
	if err != nil {
		return err
	}
	if err := e.configureStream(sampleRate, len(samples), bitsPerSample); err != nil {
		return err
	}

	if e.buffered == 0 {
		e.bufferPTS = af.Pts()
	}
	e.appendSamples(samples)
	return e.emitFullBlocks()
}

func (e *Encoder) ReceivePacket() (*media.Packet, error) {
	if len(e.pendingQueue) == 0 {
		if e.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}
	pkt := e.pendingQueue[0]
	e.pendingQueue = e.pendingQueue[1:]
	return pkt, nil
}

func (e *Encoder) Flush() error {
	if e.configErr != nil {
		return e.configErr
	}
	if e.flushed {
		return nil
	}
	if e.buffered > 0 {
		block := cloneSampleBlock(e.buffer, 0, e.buffered)
		if err := e.enqueueBlock(block, e.bufferPTS); err != nil {
			return err
		}
		e.dropBuffered(e.buffered)
	}
	e.flushed = true
	return nil
}

func (e *Encoder) configureStream(sampleRate, channels, bitsPerSample int) error {
	if e.config.StreamableSubset && e.config.BlockSize > streamableMaxBlockSize(sampleRate) {
		return fmt.Errorf("FLAC streamable-subset block size %d exceeds %d at %d Hz", e.config.BlockSize, streamableMaxBlockSize(sampleRate), sampleRate)
	}
	if e.config.SampleRate > 0 && sampleRate != e.config.SampleRate {
		return fmt.Errorf("FLAC encoder sample rate mismatch: got %d, want %d", sampleRate, e.config.SampleRate)
	}
	if e.config.Channels > 0 && channels != e.config.Channels {
		return fmt.Errorf("FLAC encoder channel count mismatch: got %d, want %d", channels, e.config.Channels)
	}
	if e.config.BitsPerSample > 0 && bitsPerSample != e.config.BitsPerSample {
		return fmt.Errorf("FLAC encoder bit depth mismatch: got %d, want %d", bitsPerSample, e.config.BitsPerSample)
	}
	if !e.streamSet {
		e.sampleRate = sampleRate
		e.channels = channels
		e.bitsPerSample = bitsPerSample
		e.buffer = make([][]int64, channels)
		e.streamSet = true
		return nil
	}
	if sampleRate != e.sampleRate {
		return fmt.Errorf("FLAC encoder sample rate changed: got %d, want %d", sampleRate, e.sampleRate)
	}
	if channels != e.channels {
		return fmt.Errorf("FLAC encoder channel count changed: got %d, want %d", channels, e.channels)
	}
	if bitsPerSample != e.bitsPerSample {
		return fmt.Errorf("FLAC encoder bit depth changed: got %d, want %d", bitsPerSample, e.bitsPerSample)
	}
	return nil
}

func (e *Encoder) appendSamples(samples [][]int64) {
	for ch := range samples {
		e.buffer[ch] = append(e.buffer[ch], samples[ch]...)
	}
	e.buffered += len(samples[0])
}

func (e *Encoder) emitFullBlocks() error {
	for e.buffered >= e.config.BlockSize {
		block := cloneSampleBlock(e.buffer, 0, e.config.BlockSize)
		if err := e.enqueueBlock(block, e.bufferPTS); err != nil {
			return err
		}
		e.dropBuffered(e.config.BlockSize)
		e.bufferPTS += media.Pts(e.config.BlockSize)
	}
	return nil
}

func (e *Encoder) enqueueBlock(block [][]int64, pts media.Pts) error {
	number := e.frameNumber
	if e.config.BlockingStrategy == VariableBlocking {
		number = e.sampleNumber
	}
	data, err := encodeFLACFrameWithOptions(block, e.sampleRate, e.bitsPerSample, number, frameOptions{
		maxFixedOrder:             e.config.MaxFixedOrder,
		maxLPCOrder:               e.config.MaxLPCOrder,
		maxRicePartitionOrder:     e.config.MaxRicePartitionOrder,
		enableWastedBits:          e.config.EnableWastedBits,
		enableStereoDecorrelation: e.config.EnableStereoDecorrel,
		streamableSubset:          e.config.StreamableSubset,
		variableBlocking:          e.config.BlockingStrategy == VariableBlocking,
	})
	if err != nil {
		return err
	}
	pkt := media.NewPacket(len(data))
	copy(pkt.Data(), data)
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0
	pkt.PTS = pts
	pkt.DTS = media.Dts(pts)
	e.pendingQueue = append(e.pendingQueue, pkt)
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
	return nil
}

func (e *Encoder) dropBuffered(n int) {
	for ch := range e.buffer {
		copy(e.buffer[ch], e.buffer[ch][n:])
		e.buffer[ch] = e.buffer[ch][:len(e.buffer[ch])-n]
	}
	e.buffered -= n
}
