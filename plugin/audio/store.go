package audio

import (
	"context"
	"errors"
	"unsafe"

	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

// held is the shape of one frame the store is keeping: enough to hand it back
// as the frame it was, and nothing of its samples, which are in the journal.
type held struct {
	pts     timing.OptionalPTS
	samples int
	offset  int64
}

// store keeps frames in the node-local temporary and reads them back in the
// order they arrived. A stage whose output nobody can know until more of the
// stream has passed keeps it here rather than in memory the job never granted,
// so what bounds it is the ceiling the job set rather than the length of the
// recording somebody chose to convert.
type store struct {
	journal  plugin.Scratch
	channels int
	maximum  int
	scratch  []float32
	planes   [][]float32
	records  []held
	// cursor is where the next record goes and extent is how far the journal
	// has ever been written. A stage that lets go of what it held writes the
	// next run over the last one, so the journal grows to the longest run
	// rather than to the sum of every run there ever was.
	cursor int64
	extent int64
}

var errNoTemporary = errors.New("this filter holds part of the stream and was granted no temporary store")

func newStore(journal plugin.Scratch, channels, maximum int) (*store, error) {
	if journal == nil {
		return nil, errNoTemporary
	}
	return &store{
		journal:  journal,
		channels: channels,
		maximum:  maximum,
		scratch:  make([]float32, channels*maximum),
		planes:   make([][]float32, channels),
	}, nil
}

func (s *store) len() int          { return len(s.records) }
func (s *store) at(index int) held { return s.records[index] }

// rewind lets go of what is held. The bytes are not given back -- a journal
// only ever grows -- but the next thing kept is written over them.
func (s *store) rewind() {
	s.records = s.records[:0]
	s.cursor = 0
}

// read copies one frame's planes out into the store's own scratch, letting
// observe see each of them on the way. A borrowed frame exposes its samples as
// an immutable view rather than a slice, so a reader that wants slices takes
// its own copy; a stage that has to measure what it is keeping reads it here
// rather than reading the frame a second time.
func (s *store) read(frame mediaaudio.Frame[float32], observe func(channel int, plane []float32)) ([][]float32, error) {
	samples := frame.Samples()
	if samples > s.maximum {
		return nil, errFrameTooLarge(samples, s.maximum)
	}
	for channel := range s.channels {
		values, err := frame.PlaneSamples(channel)
		if err != nil {
			return nil, err
		}
		window := s.scratch[channel*s.maximum : channel*s.maximum+samples]
		if values.CopyTo(window) != samples {
			return nil, errFilterPlanes
		}
		if observe != nil {
			observe(channel, window)
		}
		s.planes[channel] = window
	}
	return s.planes[:s.channels], nil
}

// keep writes one frame's planes end to end. Only the first plane's offset is
// recorded, because every plane of one frame is the same length and they are
// written in channel order.
func (s *store) keep(ctx context.Context, pts timing.OptionalPTS, planes [][]float32) error {
	record := held{pts: pts, samples: len(planes[0])}
	for channel, window := range planes {
		offset, err := s.write(ctx, floatBytes(window))
		if err != nil {
			return err
		}
		if channel == 0 {
			record.offset = offset
		}
	}
	s.records = append(s.records, record)
	return nil
}

// write puts one plane where the cursor is. It reuses bytes already written
// when what it is writing fits inside them, and grows the journal when it does
// not; a record that would straddle the end starts a new one rather than being
// split, which costs a little slack and keeps the read side a single range.
func (s *store) write(ctx context.Context, data []byte) (int64, error) {
	if s.cursor+int64(len(data)) <= s.extent {
		offset := s.cursor
		if err := s.journal.WriteAt(ctx, data, offset); err != nil {
			return 0, err
		}
		s.cursor = offset + int64(len(data))
		return offset, nil
	}
	offset, err := s.journal.Append(ctx, data)
	if err != nil {
		return 0, err
	}
	s.cursor = offset + int64(len(data))
	s.extent = s.cursor
	return offset, nil
}

// recall reads back the frame at index, writing its samples into planes the
// caller has already leased at the recorded length.
func (s *store) recall(ctx context.Context, index int, planes [][]float32) (held, error) {
	if index < 0 || index >= len(s.records) {
		return held{}, errors.New("filter recalled a frame it never held")
	}
	record := s.records[index]
	stride := int64(record.samples) * 4
	for channel := range s.channels {
		target := planes[channel][:record.samples]
		if err := s.journal.ReadAt(ctx, floatBytes(target), record.offset+int64(channel)*stride); err != nil {
			return held{}, err
		}
	}
	return record, nil
}

// floatBytes views samples as the bytes the journal stores. The journal is
// node-local scratch read back by the same component in the same process, so
// the byte order is the one it was written with by construction.
func floatBytes(values []float32) []byte {
	if len(values) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&values[0])), len(values)*4)
}
