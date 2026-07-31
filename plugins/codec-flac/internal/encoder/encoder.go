package encoder

import (
	"errors"

	"github.com/godexture/godec/plugins/codec-flac/internal/config"
	"github.com/godexture/godec/plugins/codec-flac/internal/flac"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugins/format-flac/streaminfo"
	"github.com/godexture/godec/sdk/bits"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/parallel"
	"github.com/godexture/godec/sdk/pool"
)

type Encoder struct {
	config config.EncoderConfig

	pendingQueue []*pendingEntry
	flushed      bool

	buffer        [][]int64
	bufferStart   int
	blockView     [][]int64
	buffered      int
	bufferPTS     media.Pts
	streamSet     bool
	sampleRate    int
	channels      int
	bitsPerSample int
	frameNumber   uint64
	sampleNumber  uint64
	md5           *flac.PCMMD5

	// Sequential path (pool == nil): reused across the whole stream, no
	// goroutines or channels involved. This is the pre-parallel code path,
	// kept byte-for-byte so a nil pool stays free of parallel overhead.
	windows windowSet
	writer  bits.Writer

	// Parallel path (pool != nil): full (and, when flushing, the trailing
	// partial) blocks are dispatched as tasks to a worker pool shared with
	// every other parallel-eligible stage in the conversion. Each task
	// borrows a scratch bits.Writer/windowSet from scratch so concurrent
	// tasks never contend. Parallelism never changes the encoded bytes; only
	// scheduling changes. gate implements the completion-notification side
	// of that (see markReady/waitForEntry in parallel.go), matching the
	// sibling decoder package.
	pool      *registry.WorkerPool
	scratch   pool.Typed[*encodeScratch]
	blockPool pool.Typed[*blockCopy]
	closed    bool
	gate      parallel.Gate
}

// NewEncoder builds an encoder. workers may be nil, in which case blocks are
// encoded synchronously; otherwise it must be a pool this encoder is allowed
// to submit work to for its entire lifetime (the caller retains ownership and
// is responsible for closing it once every stage sharing it has finished).
func NewEncoder(stream media.StreamInfo, cfg config.EncoderConfig, workers *registry.WorkerPool) *Encoder {
	cfg = config.MergeEncoderConfigForFactory(cfg, stream)
	e := &Encoder{
		config:  cfg,
		windows: newWindowSet(cfg.Apodizations),
		md5:     flac.NewPCMMD5(),
		pool:    workers,
	}
	e.scratch.Init(func() *encodeScratch { return &encodeScratch{windows: newWindowSet(cfg.Apodizations)} })
	e.blockPool.Init(func() *blockCopy { return &blockCopy{} })
	return e
}

func (e *Encoder) Prepare(resources registry.ResourceGrant) error {
	if e.closed {
		return errors.New("flac encoder is closed")
	}
	if len(e.pendingQueue) != 0 || e.buffered != 0 {
		return errors.New("flac encoder cannot change resources after processing starts")
	}
	if e.pool == nil {
		e.pool = resources.Pool
	}
	return nil
}

func (e *Encoder) SendFrame(frame *media.Frame) error {
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

	sampleRate, channels, bitsPerSample, err := e.audioFrameParameters(af)
	if err != nil {
		return err
	}
	if err := e.configureStream(sampleRate, channels, bitsPerSample); err != nil {
		return err
	}

	if e.buffered == 0 {
		e.bufferPTS = af.Pts()
	}
	if err := e.appendAudioFrame(af); err != nil {
		return err
	}
	return e.emitFullBlocks()
}

// ReceivePacket drains pendingQueue in submission order. Entries produced by
// the parallel path may still be in flight (entry.ready == false), in which
// case waitForEntry blocks on that specific entry until a worker finishes it.
//
// It deliberately never returns ErrEAGAIN just because the head isn't ready
// yet: callers may wire this encoder into a pipeline with fan-out (e.g. a
// tee feeding both this encoder and a comparison against its own output),
// where upstream progress depends on the encoder actually producing a
// packet rather than the caller pulling more input first. Any policy that
// withholds a dispatched-but-pending packet in favor of "let more work
// queue up" can deadlock such a graph — see the regression that motivated
// this comment. This does mean input chunked smaller than BlockSize (one
// job dispatched per full block) gets pipelined roughly one job deep rather
// than the configured parallelism; blocks dispatched together (e.g. a decoder frame
// larger than BlockSize) still encode in parallel within a single
// emitFullBlocks call.
func (e *Encoder) ReceivePacket() (*media.Packet, error) {
	for len(e.pendingQueue) > 0 {
		head := e.pendingQueue[0]
		e.waitForEntry(head)
		if head.err != nil {
			e.popPendingEntry()
			return nil, head.err
		}
		if len(head.packets) == 0 {
			e.popPendingEntry()
			continue
		}
		pkt := head.packets[0]
		head.packets[0] = nil
		head.packets = head.packets[1:]
		if len(head.packets) == 0 {
			e.popPendingEntry()
		}
		return pkt, nil
	}
	if e.flushed {
		return nil, engine.ErrEOF
	}
	return nil, engine.ErrEAGAIN
}

// popPendingEntry removes the drained head entry, resetting pendingQueue to
// nil (not just an empty slice) once it empties out, matching the
// pre-parallel contract callers rely on.
func (e *Encoder) popPendingEntry() {
	e.pendingQueue = e.pendingQueue[1:]
	if len(e.pendingQueue) == 0 {
		e.pendingQueue = nil
	}
}

func (e *Encoder) Flush() error {
	if e.flushed {
		return nil
	}
	if e.buffered > 0 {
		block := e.currentBlock(e.buffered)
		if e.pool != nil {
			e.dispatchPartialBlock(block, e.bufferPTS)
		} else if err := e.enqueueBlockSync(block, e.bufferPTS, nil); err != nil {
			return err
		}
		e.dropBuffered(e.buffered)
	}
	e.flushed = true
	sum := e.MD5()
	e.gate.Lock()
	e.pendingQueue = append(e.pendingQueue, &pendingEntry{ready: true, packets: []*media.Packet{
		media.NewPacketEvent(media.PacketKindStreamEnd, 0, []media.CodecParameters{
			media.NewCodecParameters[streaminfo.PCMMD5Parameters](sum[:]),
		}),
	}})
	e.gate.Unlock()
	return nil
}

// Close stops accepting further work without emitting a final packet or MD5
// summary. It exists so the pipeline wrapper (pkg/engine) can guarantee
// in-flight state is released on error/cancellation exits, which never reach
// Flush's io.EOF-only call site. It does not close the pool: that is shared
// with other stages and owned by whoever constructed this encoder. Safe
// whether or not Flush ran first, and safe to call more than once.
func (e *Encoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	e.flushed = true
	for _, entry := range e.pendingQueue {
		if entry != nil {
			// Wait for this encoder's own outstanding task, if any, so its
			// packets exist before being released. This never waits on other
			// stages' work: the pool is shared, but waitForEntry only
			// depends on this entry's own ready flag.
			e.waitForEntry(entry)
			releasePackets(entry.packets)
			entry.packets = nil
		}
	}
	e.pendingQueue = nil
	return nil
}

func (e *Encoder) MD5() [16]byte {
	return e.md5.Sum()
}

func newFramePacket(data []byte, pts media.Pts) *media.Packet {
	pkt := media.NewPacketFromData(data)
	pkt.MediaType = media.MediaAudio
	pkt.StreamIndex = 0
	pkt.PTS = pts
	pkt.DTS = media.Dts(pts)
	return pkt
}
