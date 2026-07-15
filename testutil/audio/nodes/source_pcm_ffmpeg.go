package nodes

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/sdk/testutil/audio/pcm"
)

type ffmpegPCMSourceNode struct {
	out       *node.OutPort[media.Frame]
	mediaPath string
	attrs     media.AudioAttributes
}

func NewFFmpegPCMSource(mediaPath string, attrs media.AudioAttributes) *ffmpegPCMSourceNode {
	return &ffmpegPCMSourceNode{
		out:       node.NewOutPort[media.Frame]("out", media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Audio: attrs}}),
		mediaPath: mediaPath,
		attrs:     attrs,
	}
}

func (n *ffmpegPCMSourceNode) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": n.out}
}

func (n *ffmpegPCMSourceNode) Start(ctx context.Context) error {
	out := n.out.Edge()
	if out == nil {
		return fmt.Errorf("FFmpeg PCM source output not connected")
	}
	defer out.Close()
	channels := n.attrs.ChannelLayout.ChannelCount()
	if channels <= 0 {
		return fmt.Errorf("invalid FFmpeg PCM channel count: %d", channels)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-loglevel", "error", "-i", n.mediaPath,
		"-map", "0:a:0", "-c:a", "pcm_f32le", "-f", "f32le", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	raw := make([]byte, pcmFramesPerChunk*channels*4)
	pcmData := make([]float32, pcmFramesPerChunk*channels)
	for {
		read, readErr := io.ReadFull(stdout, raw)
		if read > 0 {
			if read%4 != 0 || (read/4)%channels != 0 {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return fmt.Errorf("unexpected FFmpeg PCM output length: %d", read)
			}
			samples := read / 4
			for i := 0; i < samples; i++ {
				pcmData[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4 : i*4+4]))
			}
			frame, err := pcm.CreateAudioFrame(pcmData[:samples], n.attrs)
			if err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return err
			}
			if err := out.Push(ctx, *frame); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return err
			}
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return readErr
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg command failed: %w, stderr: %s", err, stderr.String())
	}
	return nil
}
