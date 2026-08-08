package audio

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/dsp"
	"github.com/godexture/godec/sdk/pool"
)

// Spool retains decoded audio with bounded RAM and transparently spills samples to a temporary file.
type Spool struct {
	limit int64
	dir   string

	layout  media.ChannelLayout
	rate    int
	set     bool
	bytes   int64
	memory  []Block
	records []Record
	file    *os.File
	reader  *bufio.Reader
	index   int
}

func NewSpool(limit int64, dir string) *Spool { return &Spool{limit: limit, dir: dir} }

func (s *Spool) Append(block Block) error {
	if !s.set {
		s.layout, s.rate, s.set = block.Layout, block.Rate, true
	} else if s.layout != block.Layout || s.rate != block.Rate {
		return fmt.Errorf("buffered audio format changed within stream")
	}
	clone := block.Clone()
	s.records = append(s.records, Record{PTS: clone.PTS, Samples: clone.Samples()})
	size := int64(clone.Samples()*len(clone.Channels)) * 4
	if s.file == nil && s.bytes+size <= s.limit {
		s.memory = append(s.memory, clone)
		s.bytes += size
		return nil
	}
	if s.file == nil {
		if err := s.open(); err != nil {
			return err
		}
		for _, item := range s.memory {
			if err := writeBlock(s.file, item); err != nil {
				return err
			}
		}
		s.memory = nil
	}
	return writeBlock(s.file, clone)
}

func (s *Spool) Rewind() error {
	s.index = 0
	if s.file == nil {
		return nil
	}
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	s.reader = bufio.NewReaderSize(s.file, 64<<10)
	return nil
}

func (s *Spool) FirstPTS() media.Pts {
	if len(s.records) == 0 {
		return 0
	}
	return s.records[0].PTS
}

func (s *Spool) Next() (Block, bool, error) {
	if s.index >= len(s.records) {
		return Block{}, false, nil
	}
	if s.file == nil {
		block := s.memory[s.index].Clone()
		s.index++
		return block, true, nil
	}
	rec := s.records[s.index]
	channels := make([][]float32, s.layout.ChannelCount())
	for i := range channels {
		channels[i] = make([]float32, rec.Samples)
	}
	block := Block{Channels: channels, Layout: s.layout, Rate: s.rate, PTS: rec.PTS}
	if err := readBlock(s.reader, block); err != nil {
		return Block{}, false, err
	}
	s.index++
	return block, true, nil
}

func (s *Spool) Close() error {
	if s.file == nil {
		return nil
	}
	name := s.file.Name()
	err := s.file.Close()
	removeErr := os.Remove(name)
	if err != nil {
		return err
	}
	return removeErr
}

func (s *Spool) open() error {
	file, err := os.CreateTemp(s.dir, "godexture-audio-*.pcm")
	if err != nil {
		return err
	}
	s.file = file
	return nil
}

const blockHeaderSize = 12 // little-endian: pts int64, sample count uint32

func writeBlock(w io.Writer, block Block) error {
	var header [blockHeaderSize]byte
	binary.LittleEndian.PutUint64(header[0:8], uint64(block.PTS))
	binary.LittleEndian.PutUint32(header[8:12], uint32(block.Samples()))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}

	channelBytes := block.Samples() * 4
	total := channelBytes * len(block.Channels)
	payload := pool.Get(total)
	defer pool.Put(payload)
	*payload = (*payload)[:total]

	for i, channel := range block.Channels {
		dst := (*payload)[i*channelBytes : (i+1)*channelBytes]
		if err := dsp.FromFloat32(dst, channel, dsp.PCMF32, 32); err != nil {
			return err
		}
	}
	_, err := w.Write(*payload)
	return err
}

func readBlock(r io.Reader, block Block) error {
	var header [blockHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return err
	}
	pts := int64(binary.LittleEndian.Uint64(header[0:8]))
	samples := binary.LittleEndian.Uint32(header[8:12])
	if media.Pts(pts) != block.PTS || int(samples) != block.Samples() {
		return fmt.Errorf("invalid temporary audio spool")
	}

	channelBytes := block.Samples() * 4
	total := channelBytes * len(block.Channels)
	payload := pool.Get(total)
	defer pool.Put(payload)
	*payload = (*payload)[:total]
	if _, err := io.ReadFull(r, *payload); err != nil {
		return err
	}

	for i, channel := range block.Channels {
		src := (*payload)[i*channelBytes : (i+1)*channelBytes]
		decoded, err := dsp.ToFloat32(channel, src, dsp.PCMF32, 32)
		if err != nil {
			return err
		}
		block.Channels[i] = decoded
	}
	return nil
}
