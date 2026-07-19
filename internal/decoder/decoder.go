package decoder

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/godexture/codec-flac/internal/config"
	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/engine"
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

	workers    int
	jobs       chan frameJob
	jobsClosed bool
	closed     bool
	workerWG   sync.WaitGroup
	workspace  decodeWorkspace
}

func NewDecoder(stream media.StreamInfo, cfg config.DecoderConfig, parallelism int) *Decoder {
	if parallelism < 1 {
		parallelism = 1
	}
	decoder := &Decoder{cfg: cfg, workers: parallelism}

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
		bitsPerSample := stream.Audio.BitsPerSample
		if bitsPerSample == 0 {
			bitsPerSample = config.BitDepthFromSampleFormat(stream.Audio.Format)
		}
		decoder.info = buildStreamInfo(stream.Audio.SampleRate, stream.Audio.ChannelCount(), bitsPerSample)
		if err := streaminfo.Validate(decoder.info); err != nil {
			decoder.configErr = err
		} else {
			decoder.parsed = true
		}
	}
	return decoder
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

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("flac decoder requires a non-nil packet")
	}
	if d.flushed {
		return engine.ErrEOF
	}
	if d.jobsClosed {
		return engine.ErrEOF
	}
	entry := &pendingEntry{done: make(chan struct{})}
	if d.workers <= 1 {
		entry.done = nil
		decodeJob(frameJob{
			data:   pkt.Data(),
			info:   d.info,
			strict: d.cfg.Strict,
			entry:  entry,
		}, &d.workspace)
		d.pendingQueue = append(d.pendingQueue, entry)
		return nil
	}
	d.startWorkers()
	d.pendingQueue = append(d.pendingQueue, entry)
	d.jobs <- frameJob{
		data:   append([]byte(nil), pkt.Data()...),
		info:   d.info,
		strict: d.cfg.Strict,
		entry:  entry,
	}
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	if d.configErr != nil {
		return nil, d.configErr
	}
	if len(d.pendingQueue) == 0 {
		if d.flushed {
			if !d.endValidated {
				d.terminalErr = d.validateEnd()
				d.endValidated = true
			}
			if d.terminalErr != nil {
				return nil, d.terminalErr
			}
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	if !d.parsed {
		return nil, errors.New("flac decoder requires STREAMINFO metadata or audio attributes")
	}
	entry := d.pendingQueue[0]
	if entry.done != nil {
		<-entry.done
	}
	d.pendingQueue[0] = nil
	d.pendingQueue = d.pendingQueue[1:]
	if len(d.pendingQueue) == 0 {
		d.pendingQueue = nil
	}
	if entry.err != nil {
		return nil, entry.err
	}
	if err := d.validateFrame(entry.header); err != nil {
		return nil, err
	}
	if d.md5 != nil {
		pcm := entry.md5
		if pcm == nil {
			pcm = entry.audio.Planes()[0]
		}
		d.md5.WritePacked(pcm)
	}
	var frame media.Frame = entry.audio
	return &frame, nil
}

func (d *Decoder) Flush() error {
	d.flushed = true
	d.closeJobs()
	return nil
}

func (d *Decoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	d.flushed = true
	d.closeJobs()
	for _, entry := range d.pendingQueue {
		if entry != nil && entry.audio != nil {
			entry.audio.Release()
			entry.audio = nil
		}
	}
	d.pendingQueue = nil
	return nil
}

func (d *Decoder) closeJobs() {
	if d.jobsClosed {
		return
	}
	d.jobsClosed = true
	if d.jobs != nil {
		close(d.jobs)
		d.workerWG.Wait()
	}
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
