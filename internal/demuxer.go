package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
)

type Demuxer struct {
	r io.ReadSeeker

	header     wavHeader
	streamInfo media.StreamInfo
	meta       metadata.Bundle
	parsed     bool
	sent       bool
	bytesRead  uint64
}

func NewDemuxer(r io.ReadSeeker) (*Demuxer, error) {
	if r == nil {
		return nil, errors.New("wav demuxer requires a readable stream")
	}

	return &Demuxer{r: r}, nil
}

func (d *Demuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	if !d.parsed {
		d.meta = *metadata.NewBundle()
		header, err := parseHeader(d.r, &d.meta)
		if err != nil {
			return nil, metadata.Bundle{}, err
		}

		d.header = header
		audioFormat := header.audioFormat

		if audioFormat == wavAudioExtensible {
			if bytes.Equal(header.subFormat[4:], wavSubFormatBase) {
				audioFormat = binary.LittleEndian.Uint16(header.subFormat[0:2])
			}
		}

		codec := media.CodecLPCM
		switch audioFormat {
		case wavAudioALaw:
			codec = media.CodecPCMA
		case wavAudioULaw:
			codec = media.CodecPCMU
		}

		var layout media.ChannelLayout
		if header.audioFormat == wavAudioExtensible && header.channelMask != 0 {
			layout = media.NewNativeLayout(media.ChannelPosition(header.channelMask))
		} else {
			layout = layoutFromChannelCount(int(header.channels))
		}

		d.streamInfo = media.StreamInfo{
			Index:     0,
			Type:      media.MediaAudio,
			IsDefault: true,
			Metadata:  d.meta,
			MediaAttributes: media.MediaAttributes{
				Codec: codec,
				Audio: media.AudioAttributes{
					SampleRate:    int(header.sampleRate),
					Format:        sampleFormatFromWAV(audioFormat, header.bitsPerSample),
					ChannelLayout: layout,
				},
			},
		}
		d.parsed = true
	}

	return []media.StreamInfo{d.streamInfo}, d.meta, nil
}

func (d *Demuxer) ReadPacket() (*media.Packet, int, error) {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return nil, 0, err
		}
	}

	if uint64(d.header.dataSize) > uint64(int(^uint(0)>>1)) {
		return nil, 0, errors.New("wav data chunk is too large for memory")
	}

	if d.bytesRead >= d.header.dataSize {
		return nil, 0, io.EOF
	}

	if !d.sent {
		if _, err := d.r.Seek(d.header.dataOffset, io.SeekStart); err != nil {
			return nil, 0, fmt.Errorf("seek data chunk: %w", err)
		}
		d.sent = true
	}

	chunkSize := uint64(32768)
	if d.header.blockAlign > 0 {
		chunkSize = (chunkSize / uint64(d.header.blockAlign)) * uint64(d.header.blockAlign)
		if chunkSize == 0 {
			chunkSize = uint64(d.header.blockAlign)
		}
	}

	remaining := d.header.dataSize - d.bytesRead
	if chunkSize > remaining {
		chunkSize = remaining
	}

	packet := media.NewPacket(int(chunkSize))
	n, err := io.ReadFull(d.r, packet.Data())
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		packet.Release()
		return nil, 0, fmt.Errorf("read wav data: %w", err)
	}

	if n == 0 {
		packet.Release()
		return nil, 0, io.EOF
	}

	if n < int(chunkSize) {
		p2 := media.NewPacket(n)
		copy(p2.Data(), packet.Data()[:n])
		packet.Release()
		packet = p2
	}

	d.bytesRead += uint64(n)

	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0

	return packet, 0, nil
}

func (d *Demuxer) Seek(offset time.Duration) error {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return err
		}
	}

	targetSample := int64(offset) * int64(d.header.sampleRate) / int64(time.Second)
	targetByteOffset := targetSample * int64(d.header.blockAlign)

	if targetByteOffset > int64(d.header.dataSize) {
		targetByteOffset = int64(d.header.dataSize)
	}

	newPos := d.header.dataOffset + targetByteOffset
	if _, err := d.r.Seek(newPos, io.SeekStart); err != nil {
		return fmt.Errorf("seek data chunk: %w", err)
	}

	d.bytesRead = uint64(targetByteOffset)
	d.sent = true
	return nil
}
