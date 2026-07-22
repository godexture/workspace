package spool

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/filter-audio/internal/audio"
	"github.com/godexture/sdk/dsp"
	"github.com/godexture/sdk/pool"
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
		block := audio.CloneBlock(s.memory[s.index])
		s.index++
		return block, true, nil
	}
	rec := s.records[s.index]
	channels := make([][]float32, s.layout.ChannelCount())
	for i := range channels {
		channels[i] = make([]float32, rec.samples)
	}
	var metadata *metadata.Bundle
	if rec.metadata != nil {
		metadata = rec.metadata.Clone()
	}
	block := audio.Block{Channels: channels, Layout: s.layout, Rate: s.rate, PTS: rec.pts, Metadata: metadata}
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

const blockHeaderSize = 12 // little-endian: pts int64, sample count uint32

func writeBlock(w io.Writer, block audio.Block) error {
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

func readBlock(r io.Reader, block audio.Block) error {
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
