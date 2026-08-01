package internal

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	mediatime "github.com/godexture/godec/core/domain/time"
	"github.com/godexture/godec/plugin/wave/params"
)

const (
	wavPacketChunkSize = 32768
	wavMP3ProbeBytes   = 8192
)

type Demuxer struct {
	r io.ReadSeeker

	header     wavHeader
	streamInfo media.StreamInfo
	meta       metadata.Bundle
	parsed     bool
	sent       bool
	bytesRead  uint64
	samplePos  uint64
	timebase   mediatime.Rational

	mp3FreeFormatBytes int
}

func NewDemuxer(r io.ReadSeeker, _ DemuxerConfig) (*Demuxer, error) {
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
		d.timebase = mediatime.NewRational(1, int64(header.sampleRate))
		audioFormat := wavResolvedAudioFormat(header)
		codec, ok := codecFromWAVAudioFormat(audioFormat)
		if !ok {
			return nil, metadata.Bundle{}, fmt.Errorf("unsupported wav audio format tag: %d", audioFormat)
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
			Duration:  d.duration(),
			Metadata:  d.meta,
			MediaAttributes: media.MediaAttributes{
				Codec:           codec,
				CodecParameters: d.codecParameters(codec),
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

func (d *Demuxer) duration() time.Duration {
	if d.header.sampleRate == 0 || d.header.blockAlign == 0 {
		return 0
	}
	samplesPerBlock := uint64(d.samplesPerBlock())
	if samplesPerBlock == 0 {
		return 0
	}
	blocks := d.header.dataSize / uint64(d.header.blockAlign)
	if blocks > ^uint64(0)/samplesPerBlock {
		return 0
	}
	totalSamples := blocks * samplesPerBlock
	rate := uint64(d.header.sampleRate)
	seconds := totalSamples / rate
	if seconds > uint64((time.Duration(1<<63-1))/time.Second) {
		return 0
	}
	remainder := totalSamples % rate
	return time.Duration(seconds)*time.Second + time.Duration(remainder*uint64(time.Second)/rate)
}

func (d *Demuxer) codecParameters(codec media.CodecID) media.CodecParameters {
	if (codec != media.CodecMSADPCM && codec != media.CodecIMAADPCM) || d.header.adpcm == nil {
		return media.CodecParameters{}
	}
	return media.NewCodecParameters[params.ADPCM](d.header.adpcm.MarshalBinary())
}

func (d *Demuxer) Seek(offset time.Duration) error {
	if !d.parsed {
		if _, _, err := d.Analyze(); err != nil {
			return err
		}
	}

	samplesPerBlock := int64(d.samplesPerBlock())
	targetSample := int64(offset) * int64(d.header.sampleRate) / int64(time.Second)

	var targetByteOffset int64
	if samplesPerBlock > 1 {
		targetBlock := targetSample / samplesPerBlock
		targetByteOffset = targetBlock * int64(d.header.blockAlign)
	} else {
		targetByteOffset = targetSample * int64(d.header.blockAlign)
	}

	if targetByteOffset > int64(d.header.dataSize) {
		targetByteOffset = int64(d.header.dataSize)
	}

	newPos := d.header.dataOffset + targetByteOffset
	if _, err := d.r.Seek(newPos, io.SeekStart); err != nil {
		return fmt.Errorf("seek data chunk: %w", err)
	}

	d.bytesRead = uint64(targetByteOffset)
	if d.header.blockAlign > 0 {
		d.samplePos = uint64(targetByteOffset/int64(d.header.blockAlign)) * uint64(samplesPerBlock)
	}
	d.sent = true
	d.mp3FreeFormatBytes = 0
	return nil
}

func (d *Demuxer) samplesPerBlock() int {
	audioFormat := wavResolvedAudioFormat(d.header)
	channels := int(d.header.channels)
	blockAlign := int(d.header.blockAlign)

	switch audioFormat {
	case wavAudioPCM, wavAudioIEEEFloat, wavAudioALaw, wavAudioULaw:
		return 1
	case wavAudioMSADPCM:
		if channels == 1 {
			return (blockAlign-7)*2 + 2
		} else {
			return (blockAlign-14)*1 + 2
		}
	case wavAudioIMAADPCM:
		if channels > 0 {
			return (blockAlign-4*channels)*2/channels + 1
		}
		return 1
	case wavAudioGSM:
		return 320
	default:
		return 1
	}
}
