package spool

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/filter-audio/internal/audio"
)

type record struct {
	pts      media.Pts
	samples  int
	metadata *metadata.Bundle
}

// Blocks retains decoded audio with bounded RAM and transparently spills samples to a temporary file.
type Blocks struct {
	limit int64
	dir   string

	layout  media.ChannelLayout
	rate    int
	set     bool
	bytes   int64
	memory  []audio.Block
	records []record
	file    *os.File
	reader  *bufio.Reader
	index   int
}

func New(limit int64, dir string) *Blocks { return &Blocks{limit: limit, dir: dir} }

func (s *Blocks) Append(block audio.Block) error {
	if !s.set {
		s.layout, s.rate, s.set = block.Layout, block.Rate, true
	} else if s.layout != block.Layout || s.rate != block.Rate {
		return fmt.Errorf("buffered audio format changed within stream")
	}
	clone := audio.CloneBlock(block)
	s.records = append(s.records, record{pts: clone.PTS, samples: clone.Samples(), metadata: clone.Metadata})
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

func (s *Blocks) Rewind() error {
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

func (s *Blocks) FirstPTS() media.Pts {
	if len(s.records) == 0 {
		return 0
	}
	return s.records[0].pts
}

func (s *Blocks) Next() (audio.Block, bool, error) {
	if s.index >= len(s.records) {
		return audio.Block{}, false, nil
	}
	if s.file == nil {
		block := s.memory[s.index]
		s.index++
		return block, true, nil
	}
	rec := s.records[s.index]
	channels := make([][]float32, s.layout.ChannelCount())
	for i := range channels {
		channels[i] = make([]float32, rec.samples)
	}
	block := audio.Block{Channels: channels, Layout: s.layout, Rate: s.rate, PTS: rec.pts, Metadata: rec.metadata}
	if err := readBlock(s.reader, block); err != nil {
		return audio.Block{}, false, err
	}
	s.index++
	return block, true, nil
}

func (s *Blocks) Close() error {
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

func (s *Blocks) open() error {
	file, err := os.CreateTemp(s.dir, "godexture-audio-*.pcm")
	if err != nil {
		return err
	}
	s.file = file
	return nil
}

func writeBlock(w io.Writer, block audio.Block) error {
	if err := binary.Write(w, binary.LittleEndian, int64(block.PTS)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(block.Samples())); err != nil {
		return err
	}
	for _, channel := range block.Channels {
		for _, value := range channel {
			if err := binary.Write(w, binary.LittleEndian, math.Float32bits(value)); err != nil {
				return err
			}
		}
	}
	return nil
}

func readBlock(r io.Reader, block audio.Block) error {
	var pts int64
	var samples uint32
	if err := binary.Read(r, binary.LittleEndian, &pts); err != nil {
		return err
	}
	if err := binary.Read(r, binary.LittleEndian, &samples); err != nil {
		return err
	}
	if media.Pts(pts) != block.PTS || int(samples) != block.Samples() {
		return fmt.Errorf("invalid temporary audio spool")
	}
	for _, channel := range block.Channels {
		for i := range channel {
			var value uint32
			if err := binary.Read(r, binary.LittleEndian, &value); err != nil {
				return err
			}
			channel[i] = math.Float32frombits(value)
		}
	}
	return nil
}
