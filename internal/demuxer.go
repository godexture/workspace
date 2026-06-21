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

const (
	wavTagRIFF = "RIFF"
	wavTagWAVE = "WAVE"
	wavTagFmt  = "fmt "
	wavTagFact = "fact"
	wavTagData = "data"

	wavAudioPCM   = 1
	wavAudioIEEEF = 3
	wavAudioALaw       = 6
	wavAudioULaw       = 7
	wavAudioExtensible = 0xFFFE
)

type wavHeader struct {
	audioFormat uint16
	channels    uint16
	sampleRate  uint32
	bitsPerSamp uint16
	blockAlign  uint16

	validBits   uint16
	channelMask uint32
	subFormat   [16]byte

	numSamples  uint64

	dataOffset int64
	dataSize   uint64
}

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
		header, err := parseHeader(d.r)
		if err != nil {
			return nil, metadata.Bundle{}, err
		}

		d.header = header
		audioFormat := header.audioFormat
		
		if audioFormat == wavAudioExtensible {
			var subFormatBase = []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}
			if bytes.Equal(header.subFormat[4:], subFormatBase) {
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
			Metadata:  *metadata.NewBundle(),
			MediaAttributes: media.MediaAttributes{
				Codec: codec,
				Audio: media.AudioAttributes{
					SampleRate:    int(header.sampleRate),
					Format:        sampleFormatFromWAV(audioFormat, header.bitsPerSamp),
					ChannelLayout: layout,
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

func parseHeader(r io.ReadSeeker) (wavHeader, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return wavHeader{}, err
	}

	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return wavHeader{}, fmt.Errorf("read riff header: %w", err)
	}

	isRF64 := string(riff[0:4]) == "RF64"
	if string(riff[0:4]) != wavTagRIFF && !isRF64 {
		return wavHeader{}, errors.New("not a wav file")
	}
	if string(riff[8:12]) != wavTagWAVE {
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
		case "ds64":
			if chunkSize < 28 {
				return wavHeader{}, errors.New("wav ds64 chunk too small")
			}
			buf := make([]byte, 28)
			if _, err := io.ReadFull(r, buf); err != nil {
				return wavHeader{}, fmt.Errorf("read ds64 chunk: %w", err)
			}
			header.dataSize = binary.LittleEndian.Uint64(buf[8:16])
			header.numSamples = binary.LittleEndian.Uint64(buf[16:24])

			if chunkSize > 28 {
				if _, err := r.Seek(int64(chunkSize-28), io.SeekCurrent); err != nil {
					return wavHeader{}, fmt.Errorf("skip ds64 chunk remainder: %w", err)
				}
			}

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
			header.blockAlign = binary.LittleEndian.Uint16(buf[12:14])
			header.bitsPerSamp = binary.LittleEndian.Uint16(buf[14:16])

			if header.audioFormat == wavAudioExtensible {
				if chunkSize < 40 {
					return wavHeader{}, errors.New("wav extensible fmt chunk too small")
				}
				cbSize := binary.LittleEndian.Uint16(buf[16:18])
				if cbSize >= 22 {
					header.validBits = binary.LittleEndian.Uint16(buf[18:20])
					header.channelMask = binary.LittleEndian.Uint32(buf[20:24])
					copy(header.subFormat[:], buf[24:40])
				} else {
					return wavHeader{}, errors.New("wav extensible cbSize too small")
				}
			}

		case wavTagFact:
			if chunkSize < 4 {
				return wavHeader{}, errors.New("wav fact chunk too small")
			}
			var numSamples32 uint32
			if err := binary.Read(r, binary.LittleEndian, &numSamples32); err != nil {
				return wavHeader{}, fmt.Errorf("read fact chunk: %w", err)
			}
			if numSamples32 != 0xFFFFFFFF {
				header.numSamples = uint64(numSamples32)
			}
			if chunkSize > 4 {
				if _, err := r.Seek(int64(chunkSize-4), io.SeekCurrent); err != nil {
					return wavHeader{}, fmt.Errorf("skip fact chunk remainder: %w", err)
				}
			}

		case wavTagData:
			header.dataOffset = chunkStart
			if chunkSize == 0xFFFFFFFF && header.dataSize != 0 {
				// use ds64 dataSize
			} else {
				header.dataSize = uint64(chunkSize)
			}

			if header.audioFormat != 0 {
				break
			}

			if _, err := r.Seek(int64(header.dataSize), io.SeekCurrent); err != nil {
				return wavHeader{}, fmt.Errorf("skip data chunk: %w", err)
			}

		default:
			if _, err := r.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return wavHeader{}, fmt.Errorf("skip chunk %q: %w", chunkID, err)
			}
		}

		actualSize := uint64(chunkSize)
		if string(chunkID[:]) == wavTagData {
			actualSize = header.dataSize
		}

		if actualSize%2 == 1 {
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

	audioFormat := header.audioFormat
	if audioFormat == wavAudioExtensible {
		var subFormatBase = []byte{0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}
		if bytes.Equal(header.subFormat[4:], subFormatBase) {
			audioFormat = binary.LittleEndian.Uint16(header.subFormat[0:2])
		}
	}

	if _, err := sampleFormatFromHeader(audioFormat, header.bitsPerSamp); err != nil {
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
		case 24:
			return media.SampleFormatS24, nil
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
