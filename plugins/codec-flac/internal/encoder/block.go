package encoder

import (
	"github.com/godexture/core/domain/media"
)

func (e *Encoder) emitFullBlocks() error {
	for e.buffered >= e.config.BlockSize {
		if err := e.enqueueFullBlock(e.currentBlock(e.config.BlockSize)); err != nil {
			return err
		}
		e.dropBuffered(e.config.BlockSize)
	}
	return nil
}

func (e *Encoder) enqueueFullBlock(block [][]int64) error {
	if e.pool != nil {
		e.dispatchFullBlock(block)
		e.bufferPTS += media.Pts(len(block[0]))
		return nil
	}
	if e.config.BlockSplitDepth == 0 {
		if err := e.enqueueBlockSync(block, e.bufferPTS, nil); err != nil {
			return err
		}
		e.bufferPTS += media.Pts(len(block[0]))
		return nil
	}
	spans, err := chooseBlockSplit(block, e.bitsPerSample, e.config, &e.windows)
	if err != nil {
		return err
	}
	defer releaseBlockSpans(spans)
	for _, span := range spans {
		if err := e.enqueueBlockSync(blockSlice(block, span.offset, span.length), e.bufferPTS, span.analysis); err != nil {
			return err
		}
		e.bufferPTS += media.Pts(span.length)
	}
	return nil
}

// enqueueBlockSync is the sequential (workers <= 1) code path: it encodes
// directly using the Encoder's own writer/windows and appends an
// already-complete pendingEntry, matching the pre-parallel behavior exactly.
func (e *Encoder) enqueueBlockSync(block [][]int64, pts media.Pts, analysis *frameAnalysis) error {
	number := e.frameNumber
	if e.config.BlockSplitDepth > 0 {
		number = e.sampleNumber
	}
	var err error
	if analysis != nil {
		_, err = writeAnalyzedFrame(&e.writer, analysis, len(block[0]), e.sampleRate, e.bitsPerSample, number, true, e.config.StreamableSubset)
	} else {
		_, err = encodeFrameWithWriter(block, e.sampleRate, e.bitsPerSample, number, e.config, true, &e.windows, &e.writer)
	}
	if err != nil {
		return err
	}
	pkt := newFramePacket(e.writer.DetachBytes(), pts)
	e.pendingQueue = append(e.pendingQueue, &pendingEntry{ready: true, packets: []*media.Packet{pkt}})
	e.frameNumber++
	e.sampleNumber += uint64(len(block[0]))
	return nil
}
