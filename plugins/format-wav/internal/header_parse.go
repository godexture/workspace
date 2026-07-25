package internal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/metadata"
)

func parseHeader(r io.ReadSeeker, meta *metadata.Bundle) (wavHeader, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return wavHeader{}, err
	}

	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return wavHeader{}, fmt.Errorf("read riff header: %w", err)
	}

	isRF64 := string(riff[0:4]) == wavTagRF64
	if string(riff[0:4]) != wavTagRIFF && !isRF64 {
		return wavHeader{}, errors.New("not a wav stream")
	}
	if string(riff[8:12]) != wavTagWAVE {
		return wavHeader{}, errors.New("not a wav stream")
	}

	listMeta := metadata.NewBundle()
	id3Meta := metadata.NewBundle()

	var header wavHeader
parseChunks:
	for {
		var chunkID [4]byte
		if _, err := io.ReadFull(r, chunkID[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return wavHeader{}, fmt.Errorf("read chunk id: %w", err)
		}

		var chunkSize uint32
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return wavHeader{}, fmt.Errorf("read chunk size: %w", err)
		}

		chunkStart, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return wavHeader{}, err
		}

		switch string(chunkID[:]) {
		case wavTagDS64:
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
			if chunkSize > 10*1024*1024 {
				return wavHeader{}, errors.New("wav fmt chunk too large")
			}

			buf := make([]byte, chunkSize)
			if _, err := io.ReadFull(r, buf); err != nil {
				return wavHeader{}, fmt.Errorf("read fmt chunk: %w", err)
			}

			header.audioFormat = binary.LittleEndian.Uint16(buf[0:2])
			header.channels = binary.LittleEndian.Uint16(buf[2:4])
			header.sampleRate = binary.LittleEndian.Uint32(buf[4:8])
			header.blockAlign = binary.LittleEndian.Uint16(buf[12:14])
			header.bitsPerSample = binary.LittleEndian.Uint16(buf[14:16])

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

			audioFormat := wavResolvedAudioFormat(header)
			if audioFormat == wavAudioMSADPCM || audioFormat == wavAudioIMAADPCM {
				params, err := parseADPCMParameters(audioFormat, int(header.channels), header.blockAlign, buf[16:])
				if err != nil {
					return wavHeader{}, err
				}
				header.adpcm = &params
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

			if header.dataSize == 0xFFFFFFFFFFFFFFFF || header.dataSize == 0xFFFFFFFF {
				break parseChunks
			}

			if _, err := r.Seek(int64(header.dataSize), io.SeekCurrent); err != nil {
				return wavHeader{}, fmt.Errorf("skip data chunk: %w", err)
			}

		case wavTagLIST:
			if err := parseLISTChunk(r, chunkSize, listMeta); err != nil {
				return wavHeader{}, err
			}

		case wavTagID3, wavTagID3Upper:
			id3Buf, err := readWAVChunkPayload(r, chunkSize)
			if err != nil {
				return wavHeader{}, err
			}
			if id3Buf != nil {
				parseAndMergeID3(id3Meta, id3Buf)
			}

		case wavTagCue:
			cueBuf, err := readWAVChunkPayload(r, chunkSize)
			if err != nil {
				return wavHeader{}, err
			}
			if cueBuf != nil {
				meta.AddRaw(wavTagCue, cueBuf)
			}

		case wavTagSmpl:
			smplBuf, err := readWAVChunkPayload(r, chunkSize)
			if err != nil {
				return wavHeader{}, err
			}
			if smplBuf != nil {
				meta.AddRaw(wavTagSmpl, smplBuf)
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
				// A truncated final chunk may omit its optional padding byte.
				if !errors.Is(err, io.EOF) {
					return wavHeader{}, fmt.Errorf("skip chunk padding: %w", err)
				}
			}
		}
	}

	if header.audioFormat == 0 || header.channels == 0 || header.sampleRate == 0 {
		return wavHeader{}, errors.New("wav header missing audio parameters")
	}

	audioFormat := wavResolvedAudioFormat(header)

	if _, err := sampleFormatFromHeader(audioFormat, header.bitsPerSample); err != nil {
		return wavHeader{}, err
	}

	meta.Merge(listMeta)
	meta.Merge(id3Meta)

	return header, nil
}

func readWAVChunkPayload(r io.ReadSeeker, size uint32) ([]byte, error) {
	const maxChunkSize = 10 * 1024 * 1024
	if size > maxChunkSize {
		if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
			return nil, err
		}
		return nil, nil
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}
