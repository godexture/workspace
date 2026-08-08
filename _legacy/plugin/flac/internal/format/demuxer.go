package internal

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	mediatime "github.com/godexture/godec/core/domain/time"
	"github.com/godexture/godec/plugin/flac/internal/frame"
	"github.com/godexture/godec/plugin/flac/internal/seektable"
	"github.com/godexture/godec/plugin/flac/internal/streaminfo"
	id3 "github.com/godexture/godec/plugin/id3"
	"github.com/godexture/godec/plugin/id3/id3v1"
	vc "github.com/godexture/godec/plugin/vorbiscomment"
)

type Demuxer struct {
	r      io.ReadSeeker
	strict bool

	streamInfo     media.StreamInfo
	metadataBundle metadata.Bundle
	audioOffset    int64
	audioEnd       int64
	nativeInfo     streaminfo.StreamInfo
	scanner        *frame.Scanner
	parsed         bool
	started        bool
	samplePos      uint64
	expectedNumber uint64
	seekPoints     []seektable.Point
	pendingFrame   *pendingFrame
}

type pendingFrame struct {
	data   []byte
	header frame.Header
}

func NewDemuxer(r io.ReadSeeker, config DemuxerConfig) (*Demuxer, error) {
	if r == nil {
		return nil, errors.New("flac demuxer requires a non-nil ReadSeeker")
	}
	return &Demuxer{r: r, strict: config.Strict}, nil
}

func (d *Demuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	if d.parsed {
		return []media.StreamInfo{d.streamInfo}, d.metadataBundle, nil
	}

	info, streamInfoBlock, extraBlocks, seekPoints, audioOffset, err := parseNativeFLACHeader(d.r, d.strict)
	markerless := false
	if err != nil && !d.strict && strings.Contains(err.Error(), "not a native FLAC stream") {
		if _, seekErr := d.r.Seek(0, io.SeekStart); seekErr != nil {
			return nil, metadata.Bundle{}, seekErr
		}
		scanner, scanErr := frame.NewScanner(io.LimitReader(d.r, 1<<20), streaminfo.StreamInfo{}, frame.Options{Sync: true})
		if scanErr != nil {
			return nil, metadata.Bundle{}, scanErr
		}
		_, header, scanErr := scanner.Next()
		if scanErr != nil {
			return nil, metadata.Bundle{}, errors.New("not a native FLAC stream")
		}
		info = synthesizedStreamInfo(header)
		streamInfoBlock = streaminfo.Encode(info)
		audioOffset = scanner.FrameOffset()
		markerless = true
	}
	if err != nil && !markerless {
		return nil, metadata.Bundle{}, err
	}

	streamMetadata := metadata.NewBundle()
	streamMetadata.AddRaw(streaminfo.MetadataKey, streamInfoBlock)

	globalMetadata := metadata.NewBundle()
	if !d.strict {
		if id3Metadata, id3Err := id3.ParseReader(d.r); id3Err == nil {
			globalMetadata.Merge(id3Metadata)
		}
	}
	for _, block := range extraBlocks {
		var header [4]byte
		copy(header[:], block[:4])
		_, blockType, _ := streaminfo.ParseBlockHeader(header)
		payload := block[4:]

		switch blockType {
		case streaminfo.MetadataTypeVorbisComment:
			if err := vc.Parse(payload, globalMetadata); err != nil {
				globalMetadata.AddRaw(streaminfo.MetadataBlockKey, block)
			}
		case streaminfo.MetadataTypePicture:
			thumbnail, err := vc.ParsePicture(payload)
			if err != nil {
				globalMetadata.AddRaw(streaminfo.MetadataBlockKey, block)
				continue
			}
			thumbnails := metadata.Get[metadata.KeyThumbnail](globalMetadata)
			globalMetadata.Set(metadata.KeyThumbnail(append(thumbnails, thumbnail)))
		default:
			globalMetadata.AddRaw(streaminfo.MetadataBlockKey, block)
		}
	}
	d.streamInfo = media.StreamInfo{
		Index:     0,
		Type:      media.MediaAudio,
		IsDefault: true,
		Duration:  info.Duration(),
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
	d.audioEnd, err = d.fileSize()
	if err != nil {
		return nil, metadata.Bundle{}, err
	}
	if !d.strict && d.audioEnd >= id3v1.TagSize {
		if _, seekErr := d.r.Seek(-int64(id3v1.TagSize), io.SeekEnd); seekErr == nil {
			var tail [id3v1.TagSize]byte
			if _, readErr := io.ReadFull(d.r, tail[:]); readErr == nil && id3v1.HasTag(tail[:]) {
				d.audioEnd -= id3v1.TagSize
			}
		}
	}
	if markerless {
		d.seekPoints = nil
	} else {
		d.seekPoints = seekPoints
	}
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
		reader := io.Reader(d.r)
		if !d.strict {
			reader = io.LimitReader(d.r, d.audioEnd-currentOffset(d.r))
		}
		scanner, err := frame.NewScanner(reader, d.nativeInfo, frame.Options{Strict: d.strict})
		if err != nil {
			return nil, 0, err
		}
		d.scanner = scanner
	}
	var data []byte
	var header frame.Header
	if d.pendingFrame != nil {
		data, header = d.pendingFrame.data, d.pendingFrame.header
		d.pendingFrame = nil
	} else {
		var err error
		data, header, err = d.scanner.Next()
		if err != nil {
			return nil, 0, err
		}
	}
	packet := media.NewPacketFromData(data)
	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0
	if d.expectedNumber != header.Number {
		d.samplePos = frame.StartSample(header, d.nativeInfo)
	}
	pts := d.samplePos
	if header.BlockingStrategy {
		pts = header.Number
	}
	packet.PTS = media.Pts(pts)
	packet.DTS = media.Dts(pts)
	packet.Timebase = mediatime.Rational(*big.NewRat(1, int64(header.SampleRate)))
	d.samplePos += uint64(header.BlockSize)
	if header.BlockingStrategy {
		d.expectedNumber = header.Number + uint64(header.BlockSize)
	} else {
		d.expectedNumber = header.Number + 1
	}

	return packet, 0, nil
}

func currentOffset(r io.ReadSeeker) int64 { offset, _ := r.Seek(0, io.SeekCurrent); return offset }

func synthesizedStreamInfo(header frame.Header) streaminfo.StreamInfo {
	info := streaminfo.StreamInfo{SampleRate: header.SampleRate, Channels: header.Channels, BitsPerSample: header.BitsPerSample}
	if header.BlockingStrategy {
		info.MinBlockSize, info.MaxBlockSize = 16, 65535
	} else {
		info.MinBlockSize, info.MaxBlockSize = uint16(header.BlockSize), uint16(header.BlockSize)
	}
	return info
}
