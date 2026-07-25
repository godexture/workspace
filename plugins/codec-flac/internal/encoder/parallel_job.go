package encoder

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/bits"
)

// blockCopy is a job's private, pool-recycled copy of the block it was
// dispatched with (see acquireBlockCopy): the encoder's own e.buffer keeps
// being written to and reused as soon as dispatch returns, so async jobs
// need an independent copy rather than a view into it.
type blockCopy struct {
	channels [][]int64
}

// acquireBlockCopy returns a blockCopy holding an independent copy of
// block's samples, reusing a pooled one (grown as needed) instead of
// allocating fresh channel slices on every dispatch.
func (e *Encoder) acquireBlockCopy(block [][]int64) *blockCopy {
	bc := e.blockPool.Get()
	if cap(bc.channels) < len(block) {
		grown := make([][]int64, len(block))
		copy(grown, bc.channels)
		bc.channels = grown
	} else {
		bc.channels = bc.channels[:len(block)]
	}
	for ch := range block {
		dst := bc.channels[ch]
		if cap(dst) < len(block[ch]) {
			dst = make([]int64, len(block[ch]))
		} else {
			dst = dst[:len(block[ch])]
		}
		copy(dst, block[ch])
		bc.channels[ch] = dst
	}
	return bc
}

func (e *Encoder) releaseBlockCopy(bc *blockCopy) {
	e.blockPool.Put(bc)
}

// runJob runs on a shared pool worker, not one dedicated to this encoder, so
// it borrows scratch state for the duration of the call instead of owning it
// for a whole goroutine's lifetime.
func (e *Encoder) runJob(job frameJob) {
	scratch := e.acquireScratch()
	packets, err := e.encodeJob(job, &scratch.writer, &scratch.windows)
	e.releaseScratch(scratch)
	e.releaseBlockCopy(job.blockCopy)
	job.entry.packets = packets
	job.entry.err = err
	e.markReady(job.entry)
}

// encodeJob mirrors enqueueFullBlock+enqueueBlockSync's logic, but as a pure
// function of its job/writer/windows arguments instead of encoder state, so
// it is safe to call from multiple pool workers concurrently.
func (e *Encoder) encodeJob(job frameJob, writer *bits.Writer, windows *windowSet) ([]*media.Packet, error) {
	if job.split && e.config.BlockSplitDepth > 0 {
		spans, err := chooseBlockSplit(job.channels, e.bitsPerSample, e.config, windows)
		if err != nil {
			return nil, err
		}
		defer releaseBlockSpans(spans)
		packets := make([]*media.Packet, 0, len(spans))
		pts := job.pts
		sampleNumber := job.sampleBase
		for _, span := range spans {
			// BlockSplitEstimated leaves span.analysis nil (only the cheap
			// estimate was computed while choosing boundaries); encode it
			// the same way enqueueBlockSync's analysis==nil branch does.
			var err error
			if span.analysis != nil {
				_, err = writeAnalyzedFrame(writer, span.analysis, span.length, e.sampleRate, e.bitsPerSample, sampleNumber, true, e.config.StreamableSubset)
			} else {
				spanBlock := blockSlice(job.channels, span.offset, span.length)
				_, err = encodeFrameWithWriter(spanBlock, e.sampleRate, e.bitsPerSample, sampleNumber, e.config, true, windows, writer)
			}
			if err != nil {
				releasePackets(packets)
				return nil, err
			}
			packets = append(packets, newFramePacket(writer.DetachBytes(), pts))
			pts += media.Pts(span.length)
			sampleNumber += uint64(span.length)
		}
		return packets, nil
	}

	number := job.frameNumber
	if e.config.BlockSplitDepth > 0 {
		number = job.sampleBase
	}
	if _, err := encodeFrameWithWriter(job.channels, e.sampleRate, e.bitsPerSample, number, e.config, true, windows, writer); err != nil {
		return nil, err
	}
	return []*media.Packet{newFramePacket(writer.DetachBytes(), job.pts)}, nil
}

func releasePackets(packets []*media.Packet) {
	for _, packet := range packets {
		if packet != nil {
			packet.Release()
		}
	}
}
