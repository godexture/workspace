package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-flac/streaminfo"
)

type Demuxer struct {
	r io.ReadSeeker

	streamInfo     media.StreamInfo
	metadataBundle metadata.Bundle
	audioOffset    int64
	packetSize     int
	parsed         bool
	started        bool
}

const (
	minFLACPacketSize = 64 << 10
	maxFLACPacketSize = 1 << 20
)

func NewDemuxer(r io.ReadSeeker) (*Demuxer, error) {
	if r == nil {
		return nil, errors.New("flac demuxer requires a non-nil ReadSeeker")
	}
	return &Demuxer{r: r}, nil
}

func (d *Demuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	if d.parsed {
		return []media.StreamInfo{d.streamInfo}, d.metadataBundle, nil
	}

	info, streamInfoBlock, extraBlocks, audioOffset, err := parseNativeFLACHeader(d.r)
	if err != nil {
		return nil, metadata.Bundle{}, err
	}

	streamMetadata := metadata.NewBundle()
	streamMetadata.AddRaw(streaminfo.MetadataKey, streamInfoBlock)

	globalMetadata := metadata.NewBundle()
	for _, block := range extraBlocks {
		globalMetadata.AddRaw(streaminfo.MetadataBlockKey, block)
	}
	if info.TotalSamples > 0 && info.SampleRate > 0 {
		seconds := float64(info.TotalSamples) / float64(info.SampleRate)
		_ = seconds // reserved for a future duration key without adding precision loss here
	}

	d.streamInfo = media.StreamInfo{
		Index:     0,
		Type:      media.MediaAudio,
		IsDefault: true,
		Metadata:  *streamMetadata,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecFLAC,
			Audio: media.AudioAttributes{
				SampleRate:    info.SampleRate,
				Format:        streaminfo.SampleFormat(info.BitsPerSample),
				BitsPerSample: info.BitsPerSample,
				ChannelLayout: streaminfo.ChannelLayout(info.Channels),
			},
		},
	}
	d.metadataBundle = *globalMetadata
	d.audioOffset = audioOffset
	d.packetSize = flacPacketSize(info.MaxFrameSize)
	d.parsed = true

	return []media.StreamInfo{d.streamInfo}, d.metadataBundle, nil
}

func (d *Demuxer) ReadPacket() (*media.Packet, int, error) {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return nil, 0, err
		}
	}

	if !d.started {
		if _, err := d.r.Seek(d.audioOffset, io.SeekStart); err != nil {
			return nil, 0, fmt.Errorf("seek FLAC audio frames: %w", err)
		}
		d.started = true
	}

	packet := media.NewPacket(d.packetSize)
	n, err := io.ReadFull(d.r, packet.Data())
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		packet.Release()
		return nil, 0, fmt.Errorf("read FLAC audio frames: %w", err)
	}
	if n == 0 {
		packet.Release()
		return nil, 0, io.EOF
	}
	if n < d.packetSize {
		shortPacket := media.NewPacket(n)
		copy(shortPacket.Data(), packet.Data()[:n])
		packet.Release()
		packet = shortPacket
	}
	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0
	packet.PTS = 0
	packet.DTS = 0

	return packet, 0, nil
}

func flacPacketSize(maxFrameSize uint32) int {
	size := uint32(minFLACPacketSize)
	if maxFrameSize > size {
		size = maxFrameSize
	}
	if size >= maxFLACPacketSize {
		return maxFLACPacketSize
	}

	size--
	size |= size >> 1
	size |= size >> 2
	size |= size >> 4
	size |= size >> 8
	size |= size >> 16
	return int(size + 1)
}
