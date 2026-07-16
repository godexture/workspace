package internal

import (
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	mediatime "github.com/godexture/core/domain/time"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
)

type Demuxer struct {
	r io.ReadSeeker

	streamInfo     media.StreamInfo
	metadataBundle metadata.Bundle
	audioOffset    int64
	nativeInfo     streaminfo.StreamInfo
	scanner        *frame.Scanner
	parsed         bool
	started        bool
}

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
	d.nativeInfo = info
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

	if d.scanner == nil {
		scanner, err := frame.NewScanner(d.r, d.nativeInfo)
		if err != nil {
			return nil, 0, err
		}
		d.scanner = scanner
	}
	data, header, err := d.scanner.Next()
	if err != nil {
		return nil, 0, err
	}
	packet := media.NewPacketFromData(data)
	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0
	pts := header.Number
	if !header.BlockingStrategy {
		pts *= uint64(header.BlockSize)
	}
	packet.PTS = media.Pts(pts)
	packet.DTS = media.Dts(pts)
	packet.Timebase = mediatime.Rational(*big.NewRat(1, int64(header.SampleRate)))

	return packet, 0, nil
}
