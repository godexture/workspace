package decoder

import (
	"errors"
	"fmt"
	"log"

	"github.com/godexture/godec/plugin/flac/internal/codec/config"
	"github.com/godexture/godec/plugin/flac/internal/codec/flac"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/flac/internal/frame"
	"github.com/godexture/godec/plugin/flac/internal/streaminfo"
	"github.com/godexture/godec/sdk/parallel"
	"github.com/godexture/godec/sdk/pool"
)

type Decoder struct {
	cfg          config.DecoderConfig
	pendingQueue []*pendingEntry
	parsed       bool
	info         streaminfo.StreamInfo
	configErr    error
	flushed      bool
	endValidated bool
	terminalErr  error
	frameCount   uint64
	sampleCount  uint64
	nextSample   uint64
	positioned   bool
	startSample  uint64
	md5          *flac.PCMMD5

	// Sequential path (pool == nil): reused across the whole stream.
	workspace decodeWorkspace

	// Parallel path (pool != nil): jobs are submitted as tasks to a worker
	// pool shared with every other parallel-eligible stage in the
	// conversion. Each task borrows a scratch decodeWorkspace from scratch
	// so concurrent tasks never contend. gate implements the
	// completion-notification side of that (see markReady/waitForEntry in
	// parallel.go).
	pool    *registry.WorkerPool
	scratch pool.Typed[*decodeWorkspace]
	closed  bool
	gate    parallel.Gate
}

// NewDecoder builds a decoder. pool may be nil, in which case packets are
// decoded synchronously; otherwise it must be a pool this decoder is allowed
// to submit work to for its entire lifetime (the caller retains ownership and
// is responsible for closing it once every stage sharing it has finished).
func NewDecoder(stream media.StreamInfo, cfg config.DecoderConfig, pool *registry.WorkerPool) *Decoder {
	decoder := &Decoder{cfg: cfg, pool: pool}
	decoder.scratch.Init(func() *decodeWorkspace { return &decodeWorkspace{} })

	hasRawStreamInfo := false
	if raw, ok := stream.Metadata.GetRaw(streaminfo.MetadataKey); ok && len(raw) > 0 {
		info, err := streaminfo.Parse(raw[0])
		if err != nil {
			decoder.configErr = err
		} else {
			decoder.info = info
			decoder.parsed = true
		}
		hasRawStreamInfo = true
	}

	if !hasRawStreamInfo && (stream.Audio.SampleRate > 0 || stream.Audio.ChannelCount() > 0 || stream.Audio.Format != media.SampleFormatUnknown) {
		bitsPerSample := stream.Audio.EffectiveBitsPerSample()
		decoder.info = buildStreamInfo(stream.Audio.SampleRate, stream.Audio.ChannelCount(), bitsPerSample)
		if err := streaminfo.Validate(decoder.info); err != nil {
			decoder.configErr = err
		} else {
			decoder.parsed = true
		}
	}
	return decoder
}

func (d *Decoder) Prepare(resources registry.ResourceGrant) error {
	if d.closed {
		return errors.New("flac decoder is closed")
	}
	if len(d.pendingQueue) != 0 {
		return errors.New("flac decoder cannot change resources after processing starts")
	}
	if d.pool == nil {
		d.pool = resources.Pool
	}
	return nil
}

func buildStreamInfo(sampleRate, channels, bitsPerSample int) streaminfo.StreamInfo {
	info := streaminfo.StreamInfo{
		MinBlockSize:  16,
		MaxBlockSize:  65535,
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
	}
	if info.SampleRate <= 0 {
		info.SampleRate = 44100
	}
	if info.Channels <= 0 {
		info.Channels = 2
	}
	if info.BitsPerSample <= 0 {
		info.BitsPerSample = 16
	}
	return info
}

func (d *Decoder) Flush() error {
	d.flushed = true
	return nil
}

// Close stops accepting further work without releasing pending frames. It
// does not close the pool: that is shared with other stages and owned by
// whoever constructed this decoder. Safe whether or not Flush ran first, and
// safe to call more than once.
func (d *Decoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.flushed = true
	for _, entry := range d.pendingQueue {
		if entry != nil {
			// Wait for this decoder's own outstanding task, if any, so its
			// frame exists before being released. This never waits on other
			// stages' work: the pool is shared, but waitForEntry only
			// depends on this entry's own ready flag.
			d.waitForEntry(entry)
			if entry.audio != nil {
				entry.audio.Release()
				entry.audio = nil
			}
		}
	}
	d.pendingQueue = nil
	return nil
}

func (d *Decoder) initMD5() {
	if d.cfg.Strict && d.info.MD5 != [16]byte{} {
		d.md5 = flac.NewPCMMD5()
	}
}

func (d *Decoder) validateFrame(header frame.Header) error {
	if !d.positioned {
		d.positioned = true
		d.startSample = frame.StartSample(header, d.info)
		d.frameCount = header.Number
		d.sampleCount = d.startSample
		d.nextSample = header.Number
		if d.startSample == 0 {
			d.initMD5()
		}
	} else if header.BlockingStrategy {
		if header.Number != d.sampleCount {
			if d.cfg.Strict {
				return fmt.Errorf("unexpected FLAC sample number: got %d, want %d", header.Number, d.sampleCount)
			}
			d.reposition(header)
		}
	} else if header.Number != d.frameCount && header.Number != d.nextSample {
		if d.cfg.Strict {
			return fmt.Errorf("unexpected FLAC frame/sample number: got %d, want frame %d or sample %d", header.Number, d.frameCount, d.nextSample)
		}
		d.reposition(header)
	}
	if d.cfg.Strict && d.info.MaxBlockSize > 0 && header.BlockSize > int(d.info.MaxBlockSize) {
		return fmt.Errorf("FLAC frame block size %d exceeds STREAMINFO maximum %d", header.BlockSize, d.info.MaxBlockSize)
	}
	d.frameCount++
	d.sampleCount += uint64(header.BlockSize)
	d.nextSample = header.Number + uint64(header.BlockSize)
	return nil
}

func (d *Decoder) reposition(header frame.Header) {
	d.frameCount = header.Number
	d.sampleCount = frame.StartSample(header, d.info)
	d.nextSample = header.Number
	d.md5 = nil
}

func (d *Decoder) validateEnd() error {
	if d.positioned && d.startSample != 0 {
		return nil
	}
	if d.info.TotalSamples > 0 && d.sampleCount != d.info.TotalSamples {
		err := fmt.Errorf("FLAC sample count mismatch: got %d, want %d", d.sampleCount, d.info.TotalSamples)
		if d.cfg.Strict {
			return err
		}
		log.Printf("WARNING: %v", err)
	}
	if d.md5 == nil {
		return nil
	}
	got := d.md5.Sum()
	if got != d.info.MD5 {
		err := fmt.Errorf("FLAC PCM MD5 mismatch: got %x, want %x", got, d.info.MD5)
		if d.cfg.Strict {
			return err
		}
		log.Printf("WARNING: %v", err)
	}
	return nil
}
