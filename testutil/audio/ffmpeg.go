package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"

	"github.com/godexture/core/domain/media"
)

func resolveFFmpegCodecId(codec media.CodecID, format media.SampleFormat) (string, error) {
	switch codec {
	case media.CodecLPCM:
		switch format {
		case media.SampleFormatS16:
			return "pcm_s16le", nil
		case media.SampleFormatF32:
			return "pcm_f32le", nil
		default:
			return "", fmt.Errorf("unsupported format for LPCM: %v", format)
		}
	case media.CodecPCMU:
		return "pcm_mulaw", nil
	case media.CodecPCMA:
		return "pcm_alaw", nil
	case media.CodecMSADPCM:
		return "adpcm_ms", nil
	case media.CodecIMAADPCM:
		return "adpcm_ima_wav", nil
	case media.CodecGSM:
		return "gsm_ms", nil
	default:
		return "", fmt.Errorf("unsupported codec for FFmpeg: %v", codec)
	}
}

func runFFmpegCommand(stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("ffmpeg", append([]string{"-i", "-", "-loglevel", "error"}, args...)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = stdin

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg command failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

func EncodeWithFFmpeg(r io.Reader, codecID media.CodecID, attrs media.AudioAttributes) ([]byte, error) {
	tmp, err := os.CreateTemp("", "ffmpeg-encode-*.wav")
	if err != nil {
		log.Fatal(err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	codec, err := resolveFFmpegCodecId(codecID, attrs.Format)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve FFmpeg codec ID: %w", err)
	}

	_, err = runFFmpegCommand(
		r,
		"-c:a", codec, "-ac", strconv.Itoa(attrs.ChannelLayout.ChannelCount()), "-ar", strconv.Itoa(attrs.SampleRate),
		"-f", "wav", "-y", tmp.Name(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run ffmpeg command: %w", err)
	}

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to read temporary file: %w", err)
	}

	return data, nil
}

func DecodeWithFFmpeg(r io.Reader) ([]float32, error) {
	data, err := runFFmpegCommand(r, "-c:a", "pcm_f32le", "-f", "f32le", "-")

	if err != nil {
		return nil, fmt.Errorf("failed to run ffmpeg command: %w", err)
	}

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
