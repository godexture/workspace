package decoder

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/godexture/codec-flac/internal/config"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/hash"
)

func TestDecoder_ReceiveFrameEmptyActive(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder(media.StreamInfo{}, config.DefaultDecoderConfig, 1)
	frame, err := decoder.ReceiveFrame()
	if !errors.Is(err, engine.ErrEAGAIN) || frame != nil {
		t.Fatalf("expected ErrEAGAIN and nil frame, got err=%v, frame=%v", err, frame)
	}
}

func TestDecoder_ReceiveFrameEmptyFlushed(t *testing.T) {
	t.Parallel()
	decoder := NewDecoder(media.StreamInfo{}, config.DefaultDecoderConfig, 1)
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
	decoder := NewDecoder(media.StreamInfo{}, config.DefaultDecoderConfig, 1)
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
	decoder := NewDecoder(media.StreamInfo{}, config.DefaultDecoderConfig, 1)
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
	assertDecodeAppendixDExample1(t, data[42:], stream, config.DefaultDecoderConfig)
}

func TestDecoderValidatesStreamEndAfterPendingFrameIsConsumed(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	decoder := NewDecoder(stream, config.DecoderConfig{Strict: true}, 1)

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
	decoder := NewDecoder(stream, config.DecoderConfig{Strict: true}, 1)

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
	decoder := NewDecoder(stream, config.DefaultDecoderConfig, 1)

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

	nonStrict := NewDecoder(stream, config.DefaultDecoderConfig, 1)
	if err := nonStrict.SendPacket(media.NewPacketFromData(frameData)); err != nil {
		t.Fatal(err)
	}
	if _, err := nonStrict.ReceiveFrame(); err != nil {
		t.Fatalf("non-strict ReceiveFrame() error = %v", err)
	}

	strict := NewDecoder(stream, config.DecoderConfig{Strict: true}, 1)
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

	decoder := NewDecoder(stream, config.DefaultDecoderConfig, 1)
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
	decoder := NewDecoder(media.StreamInfo{}, config.DefaultDecoderConfig, 1)
	if err := decoder.SendPacket(packet); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.ReceiveFrame(); err == nil {
		t.Fatal("expected native stream packet rejection")
	}
}

func assertDecodeAppendixDExample1(t *testing.T, data []byte, stream media.StreamInfo, config config.DecoderConfig) {
	t.Helper()
	packet := media.NewPacket(len(data))
	copy(packet.Data(), data)
	packet.MediaType = media.MediaAudio

	decoder := NewDecoder(stream, config, 1)
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
	decoder := NewDecoder(stream, config.DefaultDecoderConfig, 1)
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
	decoder := NewDecoder(stream, config.DefaultDecoderConfig, 1)
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

func TestDecoder_ParallelismDoesNotChangeOutput(t *testing.T) {
	t.Parallel()
	want := decodeRepeatedPackets(t, 1)
	got := decodeRepeatedPackets(t, 8)
	if len(got) != len(want) {
		t.Fatalf("frame count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("frame %d differs between parallelism 1 and 8", i)
		}
	}
}

func TestDecoder_CloseReleasesLazyWorkersWithoutFlush(t *testing.T) {
	t.Parallel()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	decoder := NewDecoder(stream, config.DefaultDecoderConfig, 4)
	if decoder.jobs != nil {
		t.Fatal("worker pool started before work was submitted")
	}
	if err := decoder.SendPacket(media.NewPacketFromData(data[42:])); err != nil {
		t.Fatalf("SendPacket() error = %v", err)
	}
	jobs := decoder.jobs
	if jobs == nil {
		t.Fatal("worker pool did not start after work was submitted")
	}
	if err := decoder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if decoder.pendingQueue != nil {
		t.Fatal("Close() retained pending decoded frames")
	}
	select {
	case _, ok := <-jobs:
		if ok {
			t.Fatal("jobs channel received a value instead of reporting closed")
		}
	default:
		t.Fatal("jobs channel is not closed")
	}
	if err := decoder.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func decodeRepeatedPackets(t *testing.T, parallelism int) [][]byte {
	t.Helper()
	data := mustDecodeHex(t, "664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	decoder := NewDecoder(stream, config.DefaultDecoderConfig, parallelism)
	for range 8 {
		if err := decoder.SendPacket(media.NewPacketFromData(data[42:])); err != nil {
			t.Fatalf("SendPacket() error = %v", err)
		}
	}
	if err := decoder.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	frames := make([][]byte, 0, 8)
	for {
		decoded, err := decoder.ReceiveFrame()
		if errors.Is(err, engine.ErrEOF) {
			return frames
		}
		if err != nil {
			t.Fatalf("ReceiveFrame() error = %v", err)
		}
		audio := (*decoded).(*media.AudioFrame)
		frames = append(frames, append([]byte(nil), audio.Planes()[0]...))
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
