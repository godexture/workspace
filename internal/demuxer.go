package internal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

const (
	flacMarker = "fLaC"

	metadataTypeStreamInfo = 0
	streamInfoLength       = 34

	streamInfoMetadataKey = "flac.streaminfo"
	maxFLACChannels       = 8
)

type streamInfo struct {
	minBlockSize  uint16
	maxBlockSize  uint16
	minFrameSize  uint32
	maxFrameSize  uint32
	sampleRate    int
	channels      int
	bitsPerSample int
	totalSamples  uint64
}

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
	streamMetadata.AddRaw(streamInfoMetadataKey, streamInfoBlock)

	globalMetadata := metadata.NewBundle()
	if info.totalSamples > 0 && info.sampleRate > 0 {
		seconds := float64(info.totalSamples) / float64(info.sampleRate)
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
				SampleRate:    info.sampleRate,
				Format:        sampleFormatForBitDepth(info.bitsPerSample),
				ChannelLayout: layoutFromChannelCount(info.channels),
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

func parseNativeFLACHeader(r io.ReadSeeker) (streamInfo, []byte, int64, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return streamInfo{}, nil, 0, err
	}

	var marker [4]byte
	if _, err := io.ReadFull(r, marker[:]); err != nil {
		return streamInfo{}, nil, 0, fmt.Errorf("read FLAC marker: %w", err)
	}
	if string(marker[:]) != flacMarker {
		return streamInfo{}, nil, 0, errors.New("not a native FLAC stream")
	}

	seenStreamInfo := false
	var parsedInfo streamInfo
	var streamInfoBlock []byte
	for {
		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return streamInfo{}, nil, 0, fmt.Errorf("read FLAC metadata header: %w", err)
		}

		isLast := header[0]&0x80 != 0
		blockType := header[0] & 0x7f
		length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
		if length < 0 {
			return streamInfo{}, nil, 0, errors.New("invalid FLAC metadata length")
		}

		block := make([]byte, length)
		if _, err := io.ReadFull(r, block); err != nil {
			return streamInfo{}, nil, 0, fmt.Errorf("read FLAC metadata block: %w", err)
		}

		if blockType == metadataTypeStreamInfo {
			if seenStreamInfo {
				return streamInfo{}, nil, 0, errors.New("duplicate FLAC STREAMINFO block")
			}
			if length != streamInfoLength {
				return streamInfo{}, nil, 0, fmt.Errorf("invalid FLAC STREAMINFO length: %d", length)
			}
			info, err := parseStreamInfo(block)
			if err != nil {
				return streamInfo{}, nil, 0, err
			}
			parsedInfo = info
			streamInfoBlock = append([]byte(nil), block...)
			seenStreamInfo = true
		} else if !seenStreamInfo {
			return streamInfo{}, nil, 0, errors.New("FLAC STREAMINFO must be the first metadata block")
		}

		if isLast {
			break
		}
	}

	if !seenStreamInfo {
		return streamInfo{}, nil, 0, errors.New("missing FLAC STREAMINFO block")
	}

	audioOffset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return streamInfo{}, nil, 0, err
	}
	return parsedInfo, streamInfoBlock, audioOffset, nil
}

func parseStreamInfo(data []byte) (streamInfo, error) {
	if len(data) != streamInfoLength {
		return streamInfo{}, fmt.Errorf("invalid STREAMINFO length: %d", len(data))
	}
	info := streamInfo{
		minBlockSize:  binary.BigEndian.Uint16(data[0:2]),
		maxBlockSize:  binary.BigEndian.Uint16(data[2:4]),
		minFrameSize:  uint32(data[4])<<16 | uint32(data[5])<<8 | uint32(data[6]),
		maxFrameSize:  uint32(data[7])<<16 | uint32(data[8])<<8 | uint32(data[9]),
		sampleRate:    int(data[10])<<12 | int(data[11])<<4 | int(data[12]>>4),
		channels:      int((data[12]>>1)&0x07) + 1,
		bitsPerSample: int(((uint16(data[12])&0x01)<<4)|uint16(data[13]>>4)) + 1,
		totalSamples:  (uint64(data[13]&0x0f) << 32) | uint64(binary.BigEndian.Uint32(data[14:18])),
	}
	if info.minBlockSize == 0 || info.maxBlockSize == 0 || info.minBlockSize > info.maxBlockSize {
		return streamInfo{}, errors.New("invalid FLAC block size in STREAMINFO")
	}
	if info.sampleRate <= 0 {
		return streamInfo{}, errors.New("invalid FLAC sample rate in STREAMINFO")
	}
	if info.channels <= 0 || info.channels > maxFLACChannels {
		return streamInfo{}, fmt.Errorf("invalid FLAC channel count: %d", info.channels)
	}
	if info.bitsPerSample <= 0 || info.bitsPerSample > 32 {
		return streamInfo{}, fmt.Errorf("unsupported FLAC bit depth: %d", info.bitsPerSample)
	}
	return info, nil
}

func sampleFormatForBitDepth(bitsPerSample int) media.SampleFormat {
	if bitsPerSample <= 16 {
		return media.SampleFormatS16
	}
	return media.SampleFormatS32
}

func layoutFromChannelCount(channels int) media.ChannelLayout {
	switch channels {
	case 1:
		return media.LayoutMono1
	case 2:
		return media.LayoutStereo2_0
	case 3:
		return media.LayoutStereo3_0
	case 4:
		return media.LayoutQuad4_0
	case 5:
		return media.LayoutFront5_0
	case 6:
		return media.LayoutFront5_1
	case 7:
		return media.LayoutSide6_1
	case 8:
		return media.LayoutWide7_1
	default:
		return media.NewUnspecified(channels)
	}
}
