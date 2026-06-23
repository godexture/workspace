package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"

	pcmcodec "github.com/godexture/codec-pcm"
	"github.com/godexture/core/domain/media"
	wavformat "github.com/godexture/format-wav"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

type codecConfig struct {
	name          string
	codecID       media.CodecID
	sampleRate    int
	channelLayout media.ChannelLayout
	format        media.SampleFormat
}

var configs = []codecConfig{
	{name: "lpcm", codecID: media.CodecLPCM, sampleRate: 16000, channelLayout: media.LayoutStereo2_0, format: media.SampleFormatS16},
	{name: "pcmu", codecID: media.CodecPCMU, sampleRate: 8000, channelLayout: media.LayoutMono1, format: media.SampleFormatS16},
	{name: "pcma", codecID: media.CodecPCMA, sampleRate: 8000, channelLayout: media.LayoutMono1, format: media.SampleFormatS16},
	{name: "msadpcm", codecID: media.CodecMSADPCM, sampleRate: 8000, channelLayout: media.LayoutStereo2_0, format: media.SampleFormatS16},
	{name: "imaadpcm", codecID: media.CodecIMAADPCM, sampleRate: 8000, channelLayout: media.LayoutStereo2_0, format: media.SampleFormatS16},
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: snapshot-generator <output_dir>")
		os.Exit(1)
	}
	outputDir := filepath.Join(os.Args[1], "testdata")
	snapshotDir := filepath.Join(outputDir, "snapshots")

	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		fmt.Printf("Error creating directories: %v\n", err)
		os.Exit(1)
	}

	// Generate source PCM: 0.5 seconds at 16000Hz stereo
	srcPCM := generateSineWave(16000, 2, 0.5)

	for _, cfg := range configs {
		fmt.Printf("Generating snapshot for %s...\n", cfg.name)

		// Resample/reformat input PCM if necessary (e.g. downmix to mono for PCMU/PCMA)
		pcm := srcPCM
		if cfg.channelLayout.ChannelCount() == 1 {
			pcm = downmixToMono(pcm)
		}
		if cfg.sampleRate != 16000 {
			pcm = resample16kTo8k(pcm, cfg.channelLayout.ChannelCount())
		}

		// 1. Encode PCM to WAV bytes
		wavBytes, err := encodePCMToWAV(pcm, cfg.codecID, cfg.sampleRate, cfg.channelLayout, cfg.format)
		if err != nil {
			fmt.Printf("Failed to encode %s: %v\n", cfg.name, err)
			os.Exit(1)
		}

		// 2. Save WAV file
		wavPath := filepath.Join(outputDir, fmt.Sprintf("short_%s.wav", cfg.name))
		if err := os.WriteFile(wavPath, wavBytes, 0644); err != nil {
			fmt.Printf("Failed to write WAV file %s: %v\n", wavPath, err)
			os.Exit(1)
		}

		// 3. Decode WAV file using FFmpeg
		decodedPCM, err := decodeWAVWithFFmpeg(wavPath)
		if err != nil {
			fmt.Printf("Failed to decode %s with FFmpeg: %v\n", cfg.name, err)
			os.Exit(1)
		}

		// 4. Save decoded PCM snapshot
		snapshotPath := filepath.Join(snapshotDir, fmt.Sprintf("short_%s.snapshot", cfg.name))
		if err := testutil.SaveSnapshot(snapshotPath, decodedPCM); err != nil {
			fmt.Printf("Failed to save snapshot %s: %v\n", snapshotPath, err)
			os.Exit(1)
		}

		fmt.Printf("Successfully generated short_%s.wav and short_%s.snapshot (%d samples)\n", cfg.name, cfg.name, len(decodedPCM))
	}
}

func generateSineWave(sampleRate int, channels int, duration float64) []float32 {
	numSamples := int(float64(sampleRate) * duration)
	pcm := make([]float32, numSamples*channels)
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		v1 := float32(math.Sin(2 * math.Pi * 100.0 * t) * 0.5)
		pcm[i*channels] = v1
		if channels > 1 {
			v2 := float32(math.Sin(2 * math.Pi * 200.0 * t) * 0.5)
			pcm[i*channels+1] = v2
		}
	}
	return pcm
}

func downmixToMono(stereo []float32) []float32 {
	mono := make([]float32, len(stereo)/2)
	for i := 0; i < len(mono); i++ {
		mono[i] = (stereo[i*2] + stereo[i*2+1]) * 0.5
	}
	return mono
}

func resample16kTo8k(in []float32, channels int) []float32 {
	// Simple decimation (half the samples since 16kHz -> 8kHz is exactly 2x downsampling)
	out := make([]float32, len(in)/2)
	for i := 0; i < len(out)/channels; i++ {
		for c := 0; c < channels; c++ {
			out[i*channels+c] = in[i*2*channels+c]
		}
	}
	return out
}

func createAudioFrame(pcm []float32, format media.SampleFormat, layout media.ChannelLayout, sampleRate int) (*media.Frame, error) {
	channels := layout.ChannelCount()
	samples := len(pcm) / channels
	f := media.NewAudioFrame(format, layout, sampleRate, samples)
	plane := f.Planes()[0]

	switch format {
	case media.SampleFormatF32:
		for i, val := range pcm {
			binary.LittleEndian.PutUint32(plane[i*4:(i+1)*4], math.Float32bits(val))
		}
	case media.SampleFormatS16:
		for i, val := range pcm {
			if val > 1.0 {
				val = 1.0
			} else if val < -1.0 {
				val = -1.0
			}
			var s16 int16
			if val < 0 {
				s16 = int16(val * 32768)
			} else {
				s16 = int16(val * 32767)
			}
			binary.LittleEndian.PutUint16(plane[i*2:(i+1)*2], uint16(s16))
		}
	default:
		return nil, fmt.Errorf("unsupported format for creation: %v", format)
	}

	var frame media.Frame = f
	return &frame, nil
}

func encodePCMToWAV(pcm []float32, codecID media.CodecID, sampleRate int, layout media.ChannelLayout, format media.SampleFormat) ([]byte, error) {
	frame, err := createAudioFrame(pcm, format, layout, sampleRate)
	if err != nil {
		return nil, err
	}

	encoder := pcmcodec.NewEncoderEngine(pcmcodec.EncoderConfig{
		CodecID:   codecID,
		ByteOrder: binary.LittleEndian,
	})

	buf := testutil.NewBuffer(nil)
	muxer := wavformat.NewMuxerEngine(buf)

	stream := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: codecID,
			Audio: media.AudioAttributes{
				SampleRate:    sampleRate,
				Format:        format,
				ChannelLayout: layout,
			},
		},
	}
	if _, err := muxer.AddStream(stream); err != nil {
		return nil, err
	}
	if err := muxer.WriteHeader(); err != nil {
		return nil, err
	}

	if err := encoder.SendFrame(frame); err != nil {
		return nil, err
	}

	for {
		pkt, err := encoder.ReceivePacket()
		if err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			return nil, err
		}
		pkt.MediaType = media.MediaAudio
		if err := muxer.WritePacket(0, pkt); err != nil {
			return nil, err
		}
	}

	if err := encoder.Flush(); err != nil {
		return nil, err
	}

	for {
		pkt, err := encoder.ReceivePacket()
		if err == engine.ErrEOF || err == engine.ErrEAGAIN {
			break
		}
		if err != nil {
			return nil, err
		}
		pkt.MediaType = media.MediaAudio
		if err := muxer.WritePacket(0, pkt); err != nil {
			return nil, err
		}
	}

	if err := muxer.WriteTrailer(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func decodeWAVWithFFmpeg(wavPath string) ([]float32, error) {
	cmd := exec.Command("ffmpeg", "-loglevel", "error", "-i", wavPath, "-f", "f32le", "-")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w, stderr: %s", err, stderr.String())
	}

	data := stdout.Bytes()
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("unexpected ffmpeg output length: %d", len(data))
	}

	samples := len(data) / 4
	pcm := make([]float32, samples)
	for i := 0; i < samples; i++ {
		bits := binary.LittleEndian.Uint32(data[i*4 : (i+1)*4])
		pcm[i] = math.Float32frombits(bits)
	}

	return pcm, nil
}
