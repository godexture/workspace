package trim

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/sdk/audio"
)

// silenceTail buffers below-threshold blocks by shape only (PTS, sample
// count, metadata) instead of retaining their sample data, and reconstructs
// them as digital silence on replay. Memory use stays bounded no matter how
// long the buffered run is, at the cost of losing bit-exact reproduction of
// that silence if it turns out not to be the true end of the stream.
type silenceTail struct {
	layout  media.ChannelLayout
	rate    int
	set     bool
	records []audio.Record
	index   int
}

func (s *silenceTail) Append(block audio.Block) error {
	if !s.set {
		s.layout, s.rate, s.set = block.Layout, block.Rate, true
	}
	var meta *metadata.Bundle
	if block.Metadata != nil {
		meta = block.Metadata.Clone()
	}
	s.records = append(s.records, audio.Record{PTS: block.PTS, Samples: block.Samples(), Metadata: meta})
	return nil
}

func (s *silenceTail) Rewind() error {
	s.index = 0
	return nil
}

func (s *silenceTail) Next() (audio.Block, bool, error) {
	if s.index >= len(s.records) {
		return audio.Block{}, false, nil
	}
	rec := s.records[s.index]
	s.index++
	channels := make(audio.Channels, s.layout.ChannelCount())
	for i := range channels {
		channels[i] = make([]float32, rec.Samples)
	}
	return audio.Block{Channels: channels, Layout: s.layout, Rate: s.rate, PTS: rec.PTS, Metadata: rec.Metadata}, true, nil
}

func (s *silenceTail) Close() error { return nil }
