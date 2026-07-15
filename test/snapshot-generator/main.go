package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"

	"github.com/godexture/codec-pcm/test/config"
	"github.com/godexture/core/domain/media"
)

func ffmpegCodec(codec media.CodecID, format media.SampleFormat) (string, error) {
	switch codec {
	case media.CodecLPCM:
		switch format {
		case media.SampleFormatS16:
			return "pcm_s16le", nil
		case media.SampleFormatF32:
			return "pcm_f32le", nil
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
	}
	return "", fmt.Errorf("unsupported codec/format for FFmpeg: %v/%v", codec, format)
}

func encodeTestFile(inputPath, outputPath string, codecID media.CodecID, attrs media.AudioAttributes) error {
	codec, err := ffmpegCodec(codecID, attrs.Format)
	if err != nil {
		return err
	}
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-i", inputPath,
		"-c:a", codec, "-ac", strconv.Itoa(attrs.ChannelLayout.ChannelCount()),
		"-ar", strconv.Itoa(attrs.SampleRate), "-f", "wav", "-y", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg encode failed: %w, stderr: %s", err, output)
	}
	return nil
}

func saveDecodedSnapshot(inputPath, outputPath string) error {
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-i", inputPath,
		"-c:a", "pcm_f32le", "-f", "f32le", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	if err := cmd.Start(); err != nil {
		return err
	}
	var sample [4]byte
	for {
		_, err := io.ReadFull(stdout, sample[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
		value := math.Float32frombits(binary.LittleEndian.Uint32(sample[:]))
		if _, err := fmt.Fprintf(writer, "%f\n", value); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg decode failed: %w, stderr: %s", err, stderr.String())
	}
	return writer.Flush()
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: snapshot-generator <input_dir>")
		os.Exit(1)
	}

	for _, profile := range config.Profiles {
		fmt.Printf("Generating snapshot for %s...\n", profile.Name)

		// Test data
		testPath := config.BuildTestdataPath(profile.Name)
		if err := encodeTestFile(config.SourcePath, testPath, profile.Codec, profile.Attrs); err != nil {
			fmt.Printf("Failed to encode %s: %v\n", profile.Name, err)
			os.Exit(1)
		}

		// Decoder snapshots
		snapshotPath := config.BuildSnapshotPath(profile.Name)
		if err := saveDecodedSnapshot(testPath, snapshotPath); err != nil {
			fmt.Printf("Failed to decode %s with FFmpeg: %v\n", profile.Name, err)
			os.Exit(1)
		}
	}
}
