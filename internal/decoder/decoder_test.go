package decoder

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/hash"
)

func TestDecoder_ReceiveFrameEmptyActive(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder(media.StreamInfo{}, flac.DecoderConfig{})
	frame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) || frame != nil {
		t.Fatalf("expected ErrEAGAIN and nil frame, got err=%v, frame=%v", err, frame)
	}
}

func TestDecoder_ReceiveFrameEmptyFlushed(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder(media.StreamInfo{}, flac.DecoderConfig{})
	if err := decoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	frame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEOF) || frame != nil {
		t.Fatalf("expected ErrEOF and nil frame, got err=%v, frame=%v", err, frame)
	}
}

func TestDecoder_SendPacketAfterFlush(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder(media.StreamInfo{}, flac.DecoderConfig{})
	if err := decoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	packet := media.NewPacket(1)
	if err := decoder.SendPacket(packet); !errors.Is(err, engine.ErrEOF) {
		t.Fatalf("expected ErrEOF after flush, got %v", err)
	}
}

func TestDecoder_SendNilPacket(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder(media.StreamInfo{}, flac.DecoderConfig{})
	if err := decoder.SendPacket(nil); err == nil {
		t.Fatal("expected error for nil packet")
	}
}

func TestDecoder_DecodeRawFrameRFC9639AppendixDExample1(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	assertDecodeAppendixDExample1(t, data[42:], stream, flac.DecoderConfig{})
}

func TestDecoderValidatesStreamEndAfterPendingFrameIsConsumed(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	decoder := NewDecoder(stream, flac.DecoderConfig{Strict: true})

	packet := media.NewPacketFromData(data[42:])
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err != nil {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}
	if err := decoder.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); !errors.Is(err, engine.ErrEOF) {
		t.Fatalf("ReceiveFrame() after Flush = %v, want ErrEOF", err)
	}
}

func TestDecoderReportsMD5MismatchAtStreamEnd(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	raw := append([]byte(nil), data[8:42]...)
	raw[len(raw)-1] ^= 1
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, raw)
	decoder := NewDecoder(stream, flac.DecoderConfig{Strict: true})

	if err := decoder.SendPacket(media.NewPacketFromData(data[42:])); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err != nil {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}
	if err := decoder.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err == nil || !strings.Contains(err.Error(), "MD5") {
		t.Fatalf("ReceiveFrame() after Flush = %v, want MD5 mismatch", err)
	}
}

func TestDecoderSkipsMD5ValidationWhenNonStrict(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	raw := append([]byte(nil), data[8:42]...)
	raw[len(raw)-1] ^= 1
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, raw)
	decoder := NewDecoder(stream, flac.DecoderConfig{})

	if err := decoder.SendPacket(media.NewPacketFromData(data[42:])); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err != nil {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}
	if err := decoder.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); !errors.Is(err, engine.ErrEOF) {
		t.Fatalf("ReceiveFrame() after Flush = %v, want ErrEOF", err)
	}
}

func TestDecoderFrameCRCValidationMode(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	frameData := append([]byte(nil), data[42:]...)
	frameData[len(frameData)-1] ^= 1

	nonStrict := NewDecoder(stream, flac.DecoderConfig{})
	if err := nonStrict.SendPacket(media.NewPacketFromData(frameData)); err != nil {
		t.Fatal(err)
	}
	if _, err := nonStrict.ReceiveFrame(); err != nil {
		t.Fatalf("non-strict ReceiveFrame() error = %v", err)
	}

	strict := NewDecoder(stream, flac.DecoderConfig{Strict: true})
	if err := strict.SendPacket(media.NewPacketFromData(frameData)); err != nil {
		t.Fatal(err)
	}
	if _, err := strict.ReceiveFrame(); err == nil || !strings.Contains(err.Error(), "CRC-16") {
		t.Fatalf("strict ReceiveFrame() error = %v, want CRC-16 error", err)
	}
}

func TestDecoderAcceptsContiguousRunStartingAtNonzeroFrame(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	packetData := append([]byte(nil), data[42:]...)
	header, err := frame.ParseHeader(packetData, mustStreamInfo(t, data[8:42]))
	if err != nil {
		t.Fatal(err)
	}
	packetData[4] = 1
	packetData[header.HeaderBytes-1] = hash.CRC8(packetData[:header.HeaderBytes-1])
	crc := hash.CRC16(packetData[:len(packetData)-2])
	packetData[len(packetData)-2], packetData[len(packetData)-1] = byte(crc>>8), byte(crc)

	decoder := NewDecoder(stream, flac.DecoderConfig{})
	if err := decoder.SendPacket(media.NewPacketFromData(packetData)); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err != nil {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}
	if err := decoder.Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); !errors.Is(err, engine.ErrEOF) {
		t.Fatalf("ReceiveFrame() after Flush = %v, want ErrEOF", err)
	}
}

func mustStreamInfo(t testing.TB, data []byte) streaminfo.StreamInfo {
	t.Helper()
	info, err := streaminfo.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestDecoder_RejectsNativeStreamPacket(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	packet := media.NewPacketFromData(data)
	decoder := NewDecoder(media.StreamInfo{}, flac.DecoderConfig{})
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err == nil {
		t.Fatal("expected native stream packet rejection")
	}
}

func assertDecodeAppendixDExample1(t *testing.T, data []byte, stream media.StreamInfo, config flac.DecoderConfig) {
	t.Helper()
	packet := media.NewPacket(len(data))
	copy(packet.Data(), data)
	packet.MediaType = media.MediaAudio

	decoder := NewDecoder(stream, config)
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatalf("SendPacket() error = %v", err)
	}

	frame, err := decoder.ReceiveFrame()
	if err != nil {
		t.Fatalf("ReceiveFrame() error = %v", err)
	}
	if frame == nil || *frame == nil {
		t.Fatal("expected decoded frame")
	}

	audioFrame, ok := (*frame).(*media.AudioFrame)
	if !ok {
		t.Fatalf("expected *media.AudioFrame, got %T", *frame)
	}
	if audioFrame.Format != media.SampleFormatS16 {
		t.Fatalf("format = %s, want %s", audioFrame.Format, media.SampleFormatS16)
	}
	if audioFrame.Layout.ChannelCount() != 2 {
		t.Fatalf("channels = %d, want 2", audioFrame.Layout.ChannelCount())
	}
	if audioFrame.SampleRate != 44100 {
		t.Fatalf("sample rate = %d, want 44100", audioFrame.SampleRate)
	}
	if audioFrame.Samples != 1 {
		t.Fatalf("samples = %d, want 1", audioFrame.Samples)
	}

	want := []byte{0xf4, 0x63, 0xb0, 0x28} // left=25588, right=10416, little-endian S16
	if got := audioFrame.Planes()[0]; !bytes.Equal(got, want) {
		t.Fatalf("decoded PCM = % x, want % x", got, want)
	}
}

func TestDecoder_RejectsIncompleteFramePacket(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	decoder := NewDecoder(stream, flac.DecoderConfig{})
	frameData := data[42:]

	packet := media.NewPacketFromData(frameData[:len(frameData)-1])
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err == nil {
		t.Fatal("expected incomplete frame rejection")
	}
}

func TestDecoder_RejectsPacketWithTrailingFrame(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	decoder := NewDecoder(stream, flac.DecoderConfig{})
	packet := media.NewPacket(len(data[42:]) * 2)
	copy(packet.Data(), data[42:])
	copy(packet.Data()[len(data[42:]):], data[42:])
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatalf("SendPacket() error = %v", err)
	}
	if _, err := decoder.ReceiveFrame(); err == nil {
		t.Fatal("expected trailing packet data rejection")
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	data, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("hex.DecodeString() error = %v", err)
	}
	return data
}
