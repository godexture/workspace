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
	parsed         bool
	sent           bool
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

	info, streamInfoBlock, audioOffset, err := parseNativeFLACHeader(d.r)
	if err != nil {
		return nil, metadata.Bundle{}, err
	}

	streamMetadata := metadata.NewBundle()
	streamMetadata.AddRaw(streaminfo.MetadataKey, streamInfoBlock)

	globalMetadata := metadata.NewBundle()
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
				ChannelLayout: streaminfo.ChannelLayout(info.Channels),
			},
		},
	}
	d.metadataBundle = *globalMetadata
	d.audioOffset = audioOffset
	d.parsed = true

	return []media.StreamInfo{d.streamInfo}, d.metadataBundle, nil
}

func (d *Demuxer) ReadPacket() (*media.Packet, int, error) {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return nil, 0, err
		}
	}

	if d.sent {
		return nil, 0, io.EOF
	}

	if _, err := d.r.Seek(d.audioOffset, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("seek FLAC audio frames: %w", err)
	}

	data, err := io.ReadAll(d.r)
	if err != nil {
		return nil, 0, fmt.Errorf("read FLAC audio frames: %w", err)
	}
	if len(data) == 0 {
		return nil, 0, io.EOF
	}

	packet := media.NewPacket(len(data))
	copy(packet.Data(), data)
	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0
	packet.PTS = 0
	packet.DTS = 0

	d.sent = true
	return packet, 0, nil
}

func parseNativeFLACHeader(r io.ReadSeeker) (streaminfo.StreamInfo, []byte, int64, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return streaminfo.StreamInfo{}, nil, 0, err
	}

	var marker [4]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil {
		return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("read FLAC marker: %w", err)
	}
	if string(marker[:]) != streaminfo.Marker {
		return streaminfo.StreamInfo{}, nil, 0, errors.New("not a native FLAC stream")
	}

	seenStreamInfo := false
	var parsedInfo streaminfo.StreamInfo
	var streamInfoBlock []byte
	for {
		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("read FLAC metadata header: %w", err)
		}

		isLast, blockType, length := streaminfo.ParseBlockHeader(header)
		if length < 0 {
			return streaminfo.StreamInfo{}, nil, 0, errors.New("invalid FLAC metadata length")
		}

		block := make([]byte, length)
		if _, err := io.ReadFull(r, block); err != nil {
			return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("read FLAC metadata block: %w", err)
		}

		if blockType == streaminfo.MetadataTypeStreamInfo {
			if seenStreamInfo {
				return streaminfo.StreamInfo{}, nil, 0, errors.New("duplicate FLAC STREAMINFO block")
			}
			if length != streaminfo.Length {
				return streaminfo.StreamInfo{}, nil, 0, fmt.Errorf("invalid FLAC STREAMINFO length: %d", length)
			}
			info, err := streaminfo.Parse(block)
			if err != nil {
				return streaminfo.StreamInfo{}, nil, 0, err
			}
			parsedInfo = info
			streamInfoBlock = append([]byte(nil), block...)
			seenStreamInfo = true
		} else if !seenStreamInfo {
			return streaminfo.StreamInfo{}, nil, 0, errors.New("FLAC STREAMINFO must be the first metadata block")
		}

		if isLast {
			break
		}
	}

	if !seenStreamInfo {
		return streaminfo.StreamInfo{}, nil, 0, errors.New("missing FLAC STREAMINFO block")
	}

	audioOffset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return streaminfo.StreamInfo{}, nil, 0, err
	}
	return parsedInfo, streamInfoBlock, audioOffset, nil
}
