package mp3

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin/mp3/internal/frame"
)

type resetID struct{}

var resetKey = key.Define[resetID, bool]()

type parser struct {
	shape        flow.Shape
	buffers      *buffer.Allocator
	framing      framing
	maxFrame     int
	signal       sample.Signal
	pending      []byte
	freeHint     int
	expected     frame.Header
	haveExpected bool
	resetPending bool
	sequence     uint64
	sample       int64
	out          flow.Item[packet.Packet]
}

func parserWorkspace(maxFrame int) int {
	if maxFrame < 4 || maxFrame > (math.MaxInt-3)/2 {
		return math.MaxInt
	}
	return 2*maxFrame + 3
}

func newParser(plan parserPlan, buffers *buffer.Allocator) *parser {
	capacity := parserWorkspace(plan.maxFrame)
	if capacity == math.MaxInt {
		capacity = plan.framing.maxFrameBytes
	}
	return &parser{
		shape:    plan.shape.Clone(),
		buffers:  buffers,
		framing:  plan.framing,
		maxFrame: plan.maxFrame,
		signal:   plan.signal,
		pending:  make([]byte, 0, capacity),
		freeHint: func() int {
			if plan.hasFraming {
				return plan.framing.freeBytes
			}
			return 0
		}(),
	}
}

func (p *parser) Ports() flow.Shape { return p.shape.Clone() }
func (*parser) Close() error        { return nil }

func (p *parser) Process(ctx context.Context, input *flow.Item[packet.Chunk], output flow.Emitter[packet.Packet]) error {
	if !input.Valid() {
		return errors.New("MP3 parser received unowned chunk")
	}
	defer input.Drop()
	data := input.Value().Bytes()
	for offset := 0; offset < data.Len(); {
		if len(p.pending) == cap(p.pending) {
			progress, err := p.scan(ctx, false, output)
			if err != nil {
				return err
			}
			if len(p.pending) == cap(p.pending) && !progress {
				return fmt.Errorf("%w: parser rolling window is exhausted", ErrUnsupported)
			}
		}
		space := cap(p.pending) - len(p.pending)
		count := data.Len() - offset
		if count > space {
			count = space
		}
		part, err := data.Slice(offset, count)
		if err != nil {
			return err
		}
		old := len(p.pending)
		p.pending = p.pending[:old+count]
		if part.CopyTo(p.pending[old:]) != count {
			return ErrMalformed
		}
		offset += count
		for {
			progress, err := p.scan(ctx, false, output)
			if err != nil {
				return err
			}
			if !progress {
				break
			}
		}
	}
	return nil
}

func (p *parser) Flush(ctx context.Context, output flow.Emitter[packet.Packet]) error {
	for len(p.pending) != 0 {
		progress, err := p.scan(ctx, true, output)
		if err != nil {
			return err
		}
		if !progress {
			return fmt.Errorf("%w: %d trailing bytes do not form a complete frame", ErrPartialFrame, len(p.pending))
		}
	}
	return nil
}

func (p *parser) scan(ctx context.Context, final bool, output flow.Emitter[packet.Packet]) (bool, error) {
	if cause := context.Cause(ctx); cause != nil {
		return false, cause
	}
	if p.haveExpected && len(p.pending) >= 4 {
		if header, err := frame.Parse(p.pending); err == nil {
			if err := p.validateHeader(header); err != nil {
				return false, err
			}
		}
	}

	result, found := frame.Scan(p.pending, p.freeHint, final)
	if cause := context.Cause(ctx); cause != nil {
		return false, cause
	}
	if result.Offset > 0 {
		if !found && final {
			return false, nil
		}
		if !p.haveExpected {
			return false, fmt.Errorf("%w: bytes precede the first frame", ErrMalformed)
		}
		p.compact(result.Offset)
		p.resetPending = true
		if found {
			return true, nil
		}
		return true, nil
	}
	if !found {
		return false, nil
	}
	if result.FrameBytes < 4 || result.FrameBytes > len(p.pending) || !result.Header.Valid() {
		return false, fmt.Errorf("%w: scanner returned an invalid frame", ErrMalformed)
	}
	if err := p.validateHeader(result.Header); err != nil {
		return false, err
	}
	if !p.haveExpected {
		p.expected = result.Header
		p.haveExpected = true
	}
	if result.FrameBytes > p.maxFrame {
		return false, fmt.Errorf("%w: frame size %d exceeds compatible maximum %d", ErrMalformed, result.FrameBytes, p.maxFrame)
	}
	if result.Header.FreeFormat() && result.FreeFormatBytes != 0 {
		p.freeHint = result.FreeFormatBytes
	}
	if err := p.emit(ctx, result.Header, p.pending[:result.FrameBytes], output); err != nil {
		return false, err
	}
	p.compact(result.FrameBytes)
	return true, nil
}

func (p *parser) validateHeader(header frame.Header) error {
	if !header.Valid() {
		return fmt.Errorf("%w: scanner returned an invalid frame", ErrMalformed)
	}
	if p.framing.freeBytes == 0 && header.FreeFormat() {
		return fmt.Errorf("%w: free-format frame is not bounded by the stream framing", ErrUnsupported)
	}
	if p.haveExpected && (!p.expected.Compatible(header) || p.expected.ChannelMode() == frame.Mono != (header.ChannelMode() == frame.Mono)) {
		return fmt.Errorf("%w: frame header changes stream framing", ErrMalformed)
	}
	if p.framing.rate != 0 && (int(header.Version()) != int(p.framing.version) || int(header.Layer()) != int(p.framing.layer) || header.SampleRateHz() != p.framing.rate || header.FreeFormat() != (p.framing.freeBytes != 0)) {
		return fmt.Errorf("%w: frame header is incompatible with inspected framing", ErrMalformed)
	}
	if header.SampleRateHz() != p.signal.Rate {
		return fmt.Errorf("%w: frame sample rate changes stream signal", ErrMalformed)
	}
	if (p.signal.Layout.Count() == 1) != (header.ChannelMode() == frame.Mono) {
		return fmt.Errorf("%w: frame channel mode changes stream layout", ErrMalformed)
	}
	return nil
}

func (p *parser) compact(offset int) {
	if offset <= 0 {
		return
	}
	copy(p.pending, p.pending[offset:])
	p.pending = p.pending[:len(p.pending)-offset]
}

func (p *parser) emit(ctx context.Context, header frame.Header, encoded []byte, output flow.Emitter[packet.Packet]) error {
	payload, err := p.buffers.FromBytes(encoded, 1)
	if err != nil {
		return err
	}
	duration := int64(header.SamplesPerFrame())
	if duration <= 0 || p.sample > math.MaxInt64-duration {
		payload.Release()
		return fmt.Errorf("%w: packet timestamp overflows", ErrUnsupported)
	}
	var sideData side.Data
	if p.resetPending {
		var sideErr error
		sideData, sideErr = side.Add(sideData, resetKey, true)
		if sideErr != nil {
			payload.Release()
			return sideErr
		}
	}
	output.Own(&p.out, packet.NewPacket(p.sequence, timing.SomePTS(timing.NewPTS(p.sample)), timing.SomeDTS(timing.NewDTS(p.sample)), timing.SomeDuration(timing.NewDuration(duration)), payload).WithSideData(sideData))
	defer p.out.Drop()
	if err := output.Emit(ctx, &p.out); err != nil {
		return err
	}
	p.sequence++
	p.sample += duration
	p.resetPending = false
	return nil
}
