package decoder

import (
	"errors"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/pool"
)

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("flac decoder requires a non-nil packet")
	}
	if d.flushed {
		return engine.ErrEOF
	}
	if d.pool == nil {
		entry := &pendingEntry{ready: true}
		decodeJob(frameJob{
			data:   pkt.Data(),
			pts:    pkt.PTS,
			info:   d.info,
			strict: d.cfg.Strict,
			entry:  entry,
		}, &d.workspace)
		d.pendingQueue = append(d.pendingQueue, entry)
		return nil
	}
	entry := &pendingEntry{}
	d.pendingQueue = append(d.pendingQueue, entry)
	src := pkt.Data()
	dataBuf := pool.Get(len(src))
	*dataBuf = (*dataBuf)[:len(src)]
	copy(*dataBuf, src)
	job := &frameJob{
		d:       d,
		data:    *dataBuf,
		dataBuf: dataBuf,
		pts:     pkt.PTS,
		info:    d.info,
		strict:  d.cfg.Strict,
		entry:   entry,
	}
	d.pool.Submit(job)
	return nil
}

func (d *Decoder) ReceiveFrame() (media.Frame, error) {
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
	d.waitForEntry(entry)
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
	return entry.audio, nil
}
