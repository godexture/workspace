package internal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-wav/params"
)

type Muxer struct {
	w io.Writer

	streamSet     bool
	stream        media.StreamInfo
	meta          metadata.Bundle
	closed        bool
	headerWritten bool

	// Configuration Options
	forceRF64 bool

	// Seekable streaming mode
	seekable   bool
	dataSize   uint64
	headerSize int64
	startPos   int64
}

type MuxerConfig struct {
	ForceRF64 bool
}

func NewMuxer(w io.Writer, config MuxerConfig) *Muxer {
	return &Muxer{w: w, forceRF64: config.ForceRF64}
}

func (m *Muxer) AddStream(info media.StreamInfo) (int, error) {
	if m.streamSet {
		return 0, errors.New("wav muxer supports only one audio stream")
	}

	if info.Type != media.MediaAudio {
		return 0, errors.New("wav muxer expects an audio stream")
	}

	isCompressed := info.MediaAttributes.Codec == media.CodecMSADPCM || info.MediaAttributes.Codec == media.CodecIMAADPCM || info.MediaAttributes.Codec == media.CodecMP3 || info.MediaAttributes.Codec == media.CodecGSM
	if info.MediaAttributes.Audio.Format == media.SampleFormatUnknown && !isCompressed {
		return 0, errors.New("wav muxer requires an audio sample format")
	}

	m.stream = info
	m.streamSet = true
	return 0, nil
}

func (m *Muxer) SetMetadata(meta metadata.Bundle) error {
	m.meta = meta
	return nil
}

func (m *Muxer) WriteHeader() error {
	if !m.streamSet {
		return errors.New("wav muxer stream is not configured")
	}
	if m.headerWritten {
		return errors.New("wav muxer header already written")
	}

	if seeker, ok := m.w.(io.Seeker); ok {
		pos, err := seeker.Seek(0, io.SeekCurrent)
		if err != nil {
			m.seekable = false
		} else {
			m.seekable = true
			m.startPos = pos
		}

		headerBytes, err := buildWAVHeader(m.stream.MediaAttributes, 0, 0, m.forceRF64)
		if err != nil {
			return err
		}

		m.headerSize = int64(len(headerBytes))
		if _, err := m.w.Write(headerBytes); err != nil {
			return err
		}
	} else {
		m.seekable = false
		// For non-seekable streams, write header with unknown size immediately and do not buffer in memory
		headerBytes, err := buildWAVHeader(m.stream.MediaAttributes, ^uint64(0), 0, m.forceRF64)
		if err != nil {
			return err
		}
		if _, err := m.w.Write(headerBytes); err != nil {
			return err
		}
	}
	m.headerWritten = true

	return nil
}

func (m *Muxer) WritePacket(streamIndex int, pkt *media.Packet) error {
	if !m.streamSet {
		return errors.New("wav muxer stream is not configured")
	}

	if streamIndex != 0 {
		return fmt.Errorf("wav muxer only accepts stream 0, got %d", streamIndex)
	}

	if pkt == nil {
		return errors.New("wav muxer received nil packet")
	}
	if pkt.Kind == media.PacketKindStreamEnd {
		return nil
	}
	if pkt.Kind != media.PacketKindData {
		return fmt.Errorf("wav muxer unsupported packet kind: %d", pkt.Kind)
	}

	if !m.headerWritten {
		if err := m.WriteHeader(); err != nil {
			return err
		}
	}

	n, err := m.w.Write(pkt.Data())
	if err != nil {
		return err
	}
	m.dataSize += uint64(n)
	return nil
}

func buildWAVHeader(attr media.MediaAttributes, dataSize uint64, trailerSize uint64, forceRF64 bool) ([]byte, error) {
	formatTag, bitsPerSample, err := wavFormatForMediaAttributes(attr)
	if err != nil {
		return nil, err
	}

	layout := attr.Audio.ChannelLayout
	channels := layout.ChannelCount()
	if channels <= 0 {
		return nil, errors.New("wav muxer requires a valid channel layout")
	}

	sampleRate := attr.Audio.SampleRate
	if sampleRate <= 0 {
		return nil, errors.New("wav muxer requires a valid sample rate")
	}

	var adpcmParams params.ADPCM
	var blockAlign int
	samplesPerBlock := 1
	if attr.Codec == media.CodecMSADPCM || attr.Codec == media.CodecIMAADPCM {
		adpcmParams, err = adpcmParametersFromMediaAttributes(attr, channels)
		if err != nil {
			return nil, err
		}
		blockAlign = int(adpcmParams.BlockAlign)
		samplesPerBlock = int(adpcmParams.SamplesPerBlock)
	} else {
		blockAlign = channels * int(bitsPerSample/8)
	}

	var byteRate int
	if attr.Codec == media.CodecMSADPCM || attr.Codec == media.CodecIMAADPCM {
		byteRate = (sampleRate * blockAlign) / samplesPerBlock
	} else {
		byteRate = sampleRate * blockAlign
	}

	isUnknownSize := dataSize == ^uint64(0)

	pad := uint64(0)
	if !isUnknownSize && dataSize%2 == 1 {
		pad = 1
	}

	useExtensible := channels >= 3 || bitsPerSample > 16
	if !useExtensible {
		defaultLayout := layoutFromChannelCount(channels)
		if layout.Mask() != defaultLayout.Mask() {
			useExtensible = true
		}
	}

	fmtSize := uint32(16)
	if useExtensible {
		fmtSize = 40
	} else if attr.Codec == media.CodecMSADPCM {
		fmtSize = uint32(22 + len(adpcmParams.Coefficients)*4)
	} else if attr.Codec == media.CodecIMAADPCM {
		fmtSize = 20
	}

	writeFormatTag := formatTag
	if useExtensible {
		writeFormatTag = wavAudioExtensible
	}

	writeFact := writeFormatTag != wavAudioPCM

	factSize := uint32(0)
	if writeFact {
		factSize = 12
	}

	var riffSize uint64
	if isUnknownSize {
		riffSize = 0xFFFFFFFF
	} else {
		riffSize = uint64(4) + 8 + uint64(fmtSize) + uint64(factSize) + 8 + dataSize + pad + trailerSize
	}

	useRF64 := forceRF64
	if !isUnknownSize && riffSize >= 0xFFFFFFFF {
		useRF64 = true
	}

	ds64TotalSize := uint64(0)
	if useRF64 {
		ds64TotalSize = 36 // 8 bytes header + 28 bytes payload
		if !isUnknownSize {
			riffSize += ds64TotalSize
		}
	}

	var headerBuf bytes.Buffer

	if useRF64 {
		headerBuf.WriteString(wavTagRF64)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	} else {
		headerBuf.WriteString(wavTagRIFF)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(riffSize))
	}

	headerBuf.WriteString(wavTagWAVE)

	if useRF64 {
		headerBuf.WriteString(wavTagDS64)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(28))
		if isUnknownSize {
			binary.Write(&headerBuf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))
			binary.Write(&headerBuf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))
			binary.Write(&headerBuf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))
		} else {
			binary.Write(&headerBuf, binary.LittleEndian, riffSize)
			binary.Write(&headerBuf, binary.LittleEndian, dataSize)
			numSamples := uint64(0)
			if blockAlign > 0 {
				numSamples = (dataSize / uint64(blockAlign)) * uint64(samplesPerBlock)
			}
			binary.Write(&headerBuf, binary.LittleEndian, numSamples)
		}
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0)) // tableLength
	}

	headerBuf.WriteString(wavTagFmt)
	binary.Write(&headerBuf, binary.LittleEndian, fmtSize)
	binary.Write(&headerBuf, binary.LittleEndian, writeFormatTag)
	binary.Write(&headerBuf, binary.LittleEndian, uint16(channels))
	binary.Write(&headerBuf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&headerBuf, binary.LittleEndian, uint32(byteRate))
	binary.Write(&headerBuf, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&headerBuf, binary.LittleEndian, bitsPerSample)

	if useExtensible {
		// LPCM samples narrower than their container are left-justified by the
		// pcm encoder, so the significant width goes out as validBitsPerSample.
		validBits := bitsPerSample
		if effectiveBits := attr.Audio.EffectiveBitsPerSample(); (attr.Codec == media.CodecLPCM || attr.Codec == "") &&
			effectiveBits > 0 && effectiveBits < int(bitsPerSample) {
			validBits = uint16(effectiveBits)
		}
		binary.Write(&headerBuf, binary.LittleEndian, uint16(22))            // cbSize
		binary.Write(&headerBuf, binary.LittleEndian, validBits)             // validBitsPerSample
		binary.Write(&headerBuf, binary.LittleEndian, uint32(layout.Mask())) // channelMask

		binary.Write(&headerBuf, binary.LittleEndian, formatTag)
		binary.Write(&headerBuf, binary.LittleEndian, uint16(0))
		headerBuf.Write(wavSubFormatBase)
	} else if attr.Codec == media.CodecMSADPCM {
		cbSize := 4 + len(adpcmParams.Coefficients)*4
		binary.Write(&headerBuf, binary.LittleEndian, uint16(cbSize))
		binary.Write(&headerBuf, binary.LittleEndian, uint16(samplesPerBlock))
		binary.Write(&headerBuf, binary.LittleEndian, uint16(len(adpcmParams.Coefficients)))
		for _, c := range adpcmParams.Coefficients {
			binary.Write(&headerBuf, binary.LittleEndian, c.Coeff1)
			binary.Write(&headerBuf, binary.LittleEndian, c.Coeff2)
		}
	} else if attr.Codec == media.CodecIMAADPCM {
		binary.Write(&headerBuf, binary.LittleEndian, uint16(2)) // cbSize
		binary.Write(&headerBuf, binary.LittleEndian, uint16(samplesPerBlock))
	}

	if writeFact {
		headerBuf.WriteString(wavTagFact)
		binary.Write(&headerBuf, binary.LittleEndian, uint32(4))
		if isUnknownSize {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
		} else {
			numSamples := uint64(0)
			if blockAlign > 0 {
				numSamples = (dataSize / uint64(blockAlign)) * uint64(samplesPerBlock)
			}
			if useRF64 && numSamples > 0xFFFFFFFF {
				binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
			} else {
				binary.Write(&headerBuf, binary.LittleEndian, uint32(numSamples))
			}
		}
	}

	headerBuf.WriteString(wavTagData)
	if useRF64 {
		binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
	} else {
		if isUnknownSize {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(0xFFFFFFFF))
		} else {
			binary.Write(&headerBuf, binary.LittleEndian, uint32(dataSize))
		}
	}

	return headerBuf.Bytes(), nil
}

func adpcmParametersFromMediaAttributes(attr media.MediaAttributes, channels int) (params.ADPCM, error) {
	if attr.Codec != media.CodecMSADPCM && attr.Codec != media.CodecIMAADPCM {
		return params.ADPCM{}, fmt.Errorf("unsupported ADPCM codec: %s", attr.Codec)
	}
	if media.IsCodecParameters[params.ADPCM](attr.CodecParameters) {
		return params.Parse(attr.Codec, channels, attr.CodecParameters.Data)
	}
	return params.Default(attr.Codec, channels)
}

func (m *Muxer) WriteTrailer() error {
	if m.closed {
		return nil
	}
	defer func() {
		m.closed = true
	}()

	if !m.streamSet {
		return errors.New("wav muxer stream is not configured")
	}

	if !m.headerWritten {
		return errors.New("wav muxer header was never written")
	}

	if m.dataSize%2 == 1 {
		if _, err := m.w.Write([]byte{0}); err != nil {
			return err
		}
	}

	var trailerBytes []byte
	if listChunk := buildListInfoChunk(m.meta); len(listChunk) > 0 {
		trailerBytes = append(trailerBytes, listChunk...)
	}
	if id3Chunk := buildID3Chunk(m.meta); len(id3Chunk) > 0 {
		trailerBytes = append(trailerBytes, id3Chunk...)
	}
	if cueChunk := buildRawChunk(wavTagCue, m.meta); len(cueChunk) > 0 {
		trailerBytes = append(trailerBytes, cueChunk...)
	}
	if smplChunk := buildRawChunk(wavTagSmpl, m.meta); len(smplChunk) > 0 {
		trailerBytes = append(trailerBytes, smplChunk...)
	}

	if len(trailerBytes) > 0 {
		if _, err := m.w.Write(trailerBytes); err != nil {
			return err
		}
	}

	if m.seekable {
		if seeker, ok := m.w.(io.Seeker); ok {
			headerBytes, err := buildWAVHeader(m.stream.MediaAttributes, m.dataSize, uint64(len(trailerBytes)), m.forceRF64)
			if err != nil {
				return err
			}

			if int64(len(headerBytes)) != m.headerSize {
				return fmt.Errorf("wav header size changed (original: %d, new: %d), cannot overwrite in streaming mode", m.headerSize, len(headerBytes))
			}

			if _, err := seeker.Seek(m.startPos, io.SeekStart); err != nil {
				return fmt.Errorf("seek to start of wav file failed: %w", err)
			}

			if _, err := m.w.Write(headerBytes); err != nil {
				return fmt.Errorf("overwrite wav header failed: %w", err)
			}
		}
	}

	return nil
}

func wavFormatForMediaAttributes(attr media.MediaAttributes) (audioFormat uint16, bitsPerSample uint16, err error) {
	switch attr.Codec {
	case media.CodecPCMA:
		return wavAudioALaw, 8, nil
	case media.CodecPCMU:
		return wavAudioULaw, 8, nil
	case media.CodecMSADPCM:
		return wavAudioMSADPCM, 4, nil
	case media.CodecIMAADPCM:
		return wavAudioIMAADPCM, 4, nil
	case media.CodecLPCM, "":
		switch attr.Audio.Format.Packed() {
		case media.SampleFormatU8:
			return wavAudioPCM, 8, nil
		case media.SampleFormatS16:
			return wavAudioPCM, 16, nil
		case media.SampleFormatS24:
			return wavAudioPCM, 24, nil
		case media.SampleFormatS32:
			return wavAudioPCM, 32, nil
		case media.SampleFormatF32:
			return wavAudioIEEEFloat, 32, nil
		case media.SampleFormatF64:
			return wavAudioIEEEFloat, 64, nil
		default:
			return 0, 0, fmt.Errorf("unsupported wav sample format: %s", attr.Audio.Format)
		}
	default:
		return 0, 0, fmt.Errorf("unsupported wav codec: %s", attr.Codec)
	}
}
