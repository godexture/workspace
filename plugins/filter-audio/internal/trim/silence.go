package trim

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/filter-audio/internal/audio"
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
	records []silenceRecord
	index   int
}

type silenceRecord struct {
	pts      media.Pts
	samples  int
	metadata *metadata.Bundle
}

func (s *silenceTail) Append(block audio.Block) error {
	if !s.set {
		s.layout, s.rate, s.set = block.Layout, block.Rate, true
	}
	var meta *metadata.Bundle
	if block.Metadata != nil {
		meta = block.Metadata.Clone()
	}
	s.records = append(s.records, silenceRecord{pts: block.PTS, samples: block.Samples(), metadata: meta})
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
		channels[i] = make([]float32, rec.samples)
	}
	return audio.Block{Channels: channels, Layout: s.layout, Rate: s.rate, PTS: rec.pts, Metadata: rec.metadata}, true, nil
}

func (s *silenceTail) Close() error { return nil }
