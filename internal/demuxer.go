package internal

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	mp3header "github.com/godexture/format-mp3/header"
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

	mp3FreeFormatBytes int
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

	if d.streamInfo.Codec == media.CodecMP3 {
		return d.readMP3Packet()
	}

	return d.readRawPacket()
}

func (d *Demuxer) readRawPacket() (*media.Packet, int, error) {
	chunkSize := uint64(wavPacketChunkSize)
	if d.header.blockAlign > 0 {
		isADPCM := d.streamInfo.MediaAttributes.Codec == media.CodecMSADPCM || d.streamInfo.MediaAttributes.Codec == media.CodecIMAADPCM
		if isADPCM {
			chunkSize = uint64(d.header.blockAlign)
		} else {
			chunkSize = (chunkSize / uint64(d.header.blockAlign)) * uint64(d.header.blockAlign)
		}
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

func (d *Demuxer) readMP3Packet() (*media.Packet, int, error) {
	remaining := d.header.dataSize - d.bytesRead
	probeSize := remaining
	if probeSize > wavMP3ProbeBytes {
		probeSize = wavMP3ProbeBytes
	}

	probe := make([]byte, int(probeSize))
	n, err := io.ReadFull(d.r, probe)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, 0, fmt.Errorf("read wav mp3 data: %w", err)
	}
	if n == 0 {
		return nil, 0, io.EOF
	}
	probe = probe[:n]

	offset, frameBytes, freeFormatBytes, found := findMP3Frame(probe, d.mp3FreeFormatBytes)
	if !found {
		return nil, 0, errors.New("wav mp3 data does not contain a complete mp3 frame")
	}
	if offset+frameBytes > len(probe) {
		return nil, 0, errors.New("wav mp3 frame exceeds probe window")
	}

	d.mp3FreeFormatBytes = freeFormatBytes
	consumed := offset + frameBytes
	if unread := len(probe) - consumed; unread > 0 {
		if _, err := d.r.Seek(int64(-unread), io.SeekCurrent); err != nil {
			return nil, 0, fmt.Errorf("rewind wav mp3 probe: %w", err)
		}
	}

	packet := media.NewPacket(frameBytes)
	copy(packet.Data(), probe[offset:consumed])
	d.bytesRead += uint64(consumed)
	packet.MediaType = media.MediaAudio
	packet.StreamIndex = 0

	return packet, 0, nil
}

func findMP3Frame(data []byte, freeFormatBytes int) (offset int, frameBytes int, newFreeFormatBytes int, found bool) {
	for i := 0; i+4 <= len(data); i++ {
		h, err := mp3header.ParseHeader(data[i : i+4])
		if err != nil || !h.IsValid() {
			continue
		}

		currentFreeFormatBytes := freeFormatBytes
		frameBytes = h.FrameBytes(currentFreeFormatBytes)
		frameAndPadding := frameBytes + h.Padding()

		for step := 4; frameBytes == 0 && step < 2304 && i+2*step <= len(data)-4; step++ {
			nextHeader, err := mp3header.ParseHeader(data[i+step : i+step+4])
			if err != nil || !h.Compare(nextHeader) {
				continue
			}

			foundFrameBytes := step - h.Padding()
			nextFrameBytes := foundFrameBytes + nextHeader.Padding()
			if i+step+nextFrameBytes+4 > len(data) {
				continue
			}

			nextHeader2, err := mp3header.ParseHeader(data[i+step+nextFrameBytes : i+step+nextFrameBytes+4])
			if err != nil || !h.Compare(nextHeader2) {
				continue
			}

			frameAndPadding = step
			frameBytes = foundFrameBytes
			currentFreeFormatBytes = foundFrameBytes
		}

		if frameBytes == 0 || i+frameAndPadding > len(data) {
			continue
		}

		if matchMP3Frames(data[i:], h, currentFreeFormatBytes) || (i == 0 && frameAndPadding == len(data)) {
			return i, frameAndPadding, currentFreeFormatBytes, true
		}
	}

	return len(data), 0, 0, false
}

func matchMP3Frames(data []byte, first mp3header.Header, freeFormatBytes int) bool {
	byteIndex := 0
	matchCount := 0
	current := first

	for ; matchCount < 10; matchCount++ {
		byteIndex += current.FrameBytes(freeFormatBytes) + current.Padding()
		if byteIndex+4 > len(data) {
			return matchCount > 0
		}

		next, err := mp3header.ParseHeader(data[byteIndex : byteIndex+4])
		if err != nil || !first.Compare(next) {
			return false
		}
		current = next
	}

	return true
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
	d.mp3FreeFormatBytes = 0
	return nil
}
