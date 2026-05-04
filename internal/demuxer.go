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
	wavTagRIFF = "RIFF"
	wavTagWAVE = "WAVE"
	wavTagFmt  = "fmt "
	wavTagData = "data"

	wavAudioPCM   = 1
	wavAudioIEEEF = 3
	wavAudioALaw  = 6
	wavAudioULaw  = 7
)

type wavHeader struct {
	audioFormat uint16
	channels    uint16
	sampleRate  uint32
	bitsPerSamp uint16

	dataOffset int64
	dataSize   uint32
}

type Demuxer struct {
	r io.ReadSeeker

	header     wavHeader
	streamInfo media.StreamInfo
	meta       metadata.Bundle
	parsed     bool
	sent       bool
}

func NewDemuxer(r io.ReadSeeker) (*Demuxer, error) {
	if r == nil {
		return nil, errors.New("wav demuxer requires a readable stream")
	}

	return &Demuxer{r: r}, nil
}

func (d *Demuxer) Analyze() ([]media.StreamInfo, metadata.Bundle, error) {
	if !d.parsed {
		header, err := parseHeader(d.r)
		if err != nil {
			return nil, metadata.Bundle{}, err
		}

		d.header = header
		codec := media.CodecLPCM
		switch header.audioFormat {
		case wavAudioALaw:
			codec = media.CodecPCMA
		case wavAudioULaw:
			codec = media.CodecPCMU
		}

		d.streamInfo = media.StreamInfo{
			Index:     0,
			Type:      media.MediaAudio,
			IsDefault: true,
			Metadata:  *metadata.NewBundle(),
			MediaAttributes: media.MediaAttributes{
				Codec: codec,
				Audio: media.AudioAttributes{
					CodecID:       codec,
					SampleRate:    int(header.sampleRate),
					Format:        sampleFormatFromWAV(header.audioFormat, header.bitsPerSamp),
					ChannelLayout: layoutFromChannelCount(int(header.channels)),
				},
			},
		}
		d.meta = *metadata.NewBundle()
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

	if d.sent {
		return nil, 0, io.EOF
	}

	if _, err := d.r.Seek(d.header.dataOffset, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("seek data chunk: %w", err)
	}

	if uint64(d.header.dataSize) > uint64(int(^uint(0)>>1)) {
		return nil, 0, errors.New("wav data chunk is too large")
	}

	packet := media.NewPacket(int(d.header.dataSize))
	if _, err := io.ReadFull(d.r, packet.Data()); err != nil {
		packet.Release()
		return nil, 0, fmt.Errorf("read wav data: %w", err)
	}

	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0

	d.sent = true
	return packet, 0, nil
}

func parseHeader(r io.ReadSeeker) (wavHeader, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return wavHeader{}, err
	}

	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return wavHeader{}, fmt.Errorf("read riff header: %w", err)
	}

	if string(riff[0:4]) != wavTagRIFF || string(riff[8:12]) != wavTagWAVE {
		return wavHeader{}, errors.New("not a wav file")
	}

	var header wavHeader
	for {
		var chunkID [4]byte
		if _, err := io.ReadFull(r, chunkID[:]); err != nil {
			return wavHeader{}, fmt.Errorf("read chunk id: %w", err)
		}

		var chunkSize uint32
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			return wavHeader{}, fmt.Errorf("read chunk size: %w", err)
		}

		chunkStart, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return wavHeader{}, err
		}

		switch string(chunkID[:]) {
		case wavTagFmt:
			if chunkSize < 16 {
				return wavHeader{}, errors.New("wav fmt chunk too small")
			}

			buf := make([]byte, chunkSize)
			if _, err := io.ReadFull(r, buf); err != nil {
				return wavHeader{}, fmt.Errorf("read fmt chunk: %w", err)
			}

			header.audioFormat = binary.LittleEndian.Uint16(buf[0:2])
			header.channels = binary.LittleEndian.Uint16(buf[2:4])
			header.sampleRate = binary.LittleEndian.Uint32(buf[4:8])
			header.bitsPerSamp = binary.LittleEndian.Uint16(buf[14:16])

		case wavTagData:
			header.dataOffset = chunkStart
			header.dataSize = chunkSize
			if _, err := r.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return wavHeader{}, fmt.Errorf("skip data chunk: %w", err)
			}

		default:
			if _, err := r.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return wavHeader{}, fmt.Errorf("skip chunk %q: %w", chunkID, err)
			}
		}

		if chunkSize%2 == 1 {
			if _, err := r.Seek(1, io.SeekCurrent); err != nil {
				return wavHeader{}, err
			}
		}

		if header.audioFormat != 0 && header.dataOffset != 0 {
			break
		}
	}

	if header.channels == 0 || header.sampleRate == 0 || header.bitsPerSamp == 0 {
		return wavHeader{}, errors.New("wav header missing audio parameters")
	}

	if _, err := sampleFormatFromHeader(header.audioFormat, header.bitsPerSamp); err != nil {
		return wavHeader{}, err
	}

	return header, nil
}

func sampleFormatFromHeader(audioFormat, bitsPerSample uint16) (media.SampleFormat, error) {
	switch audioFormat {
	case wavAudioPCM:
		switch bitsPerSample {
		case 8:
			return media.SampleFormatU8, nil
		case 16:
			return media.SampleFormatS16, nil
		case 32:
			return media.SampleFormatS32, nil
		default:
			return media.SampleFormatUnknown, fmt.Errorf("unsupported pcm bit depth: %d", bitsPerSample)
		}

	case wavAudioIEEEF:
		switch bitsPerSample {
		case 32:
			return media.SampleFormatF32, nil
		case 64:
			return media.SampleFormatF64, nil
		default:
			return media.SampleFormatUnknown, fmt.Errorf("unsupported float bit depth: %d", bitsPerSample)
		}

	case wavAudioALaw, wavAudioULaw:
		if bitsPerSample != 8 {
			return media.SampleFormatUnknown, fmt.Errorf("unsupported g711 bit depth: %d", bitsPerSample)
		}
		return media.SampleFormatU8, nil

	default:
		return media.SampleFormatUnknown, fmt.Errorf("unsupported wav audio format tag: %d", audioFormat)
	}
}

func sampleFormatFromWAV(audioFormat uint16, bitsPerSample uint16) media.SampleFormat {
	format, err := sampleFormatFromHeader(audioFormat, bitsPerSample)
	if err != nil {
		return media.SampleFormatUnknown
	}

	return format
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
		return media.LayoutSide7_0
	case 8:
		return media.LayoutSurround7_1
	default:
		return media.NewUnspecified(channels)
	}
}
