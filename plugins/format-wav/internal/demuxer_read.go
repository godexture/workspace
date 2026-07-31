package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/godec/core/domain/media"
	mp3header "github.com/godexture/godec/plugins/format-mp3/header"
	"github.com/godexture/godec/plugins/format-mp3/scan"
)

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
		if d.samplesPerBlock() > 1 {
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
	packet.PTS = media.Pts(d.samplePos)
	packet.DTS = media.Dts(d.samplePos)
	packet.Timebase = d.timebase
	if d.header.blockAlign > 0 {
		d.samplePos += uint64(n/int(d.header.blockAlign)) * uint64(d.samplesPerBlock())
	}

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

	offset, frameBytes, freeFormatBytes, found := scan.Frame(probe, d.mp3FreeFormatBytes)
	if !found {
		if d.bytesRead+uint64(n) >= d.header.dataSize {
			return nil, 0, io.EOF
		}
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
	packet.PTS = media.Pts(d.samplePos)
	packet.DTS = media.Dts(d.samplePos)
	packet.Timebase = d.timebase
	header, err := mp3header.ParseHeader(probe[offset : offset+4])
	if err != nil {
		packet.Release()
		return nil, 0, fmt.Errorf("parse wav mp3 frame header: %w", err)
	}
	d.samplePos += uint64(header.FrameSamples())

	return packet, 0, nil
}
