package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

// CreateAudioFrame creates a new AudioFrame containing the given float32 PCM samples converted to the target format.
func CreateAudioFrame(pcm []float32, attrs media.AudioAttributes) (*media.Frame, error) {
	channels := attrs.ChannelLayout.ChannelCount()
	samples := len(pcm) / channels
	f := media.NewAudioFrame(attrs.Format, attrs.ChannelLayout, attrs.SampleRate, samples)
	plane := f.Planes()[0]

	switch attrs.Format {
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
	case media.SampleFormatS24:
		for i, val := range pcm {
			if val > 1.0 {
				val = 1.0
			} else if val < -1.0 {
				val = -1.0
			}
			value := int32(val * 8388608.0)
			if value > 8388607 {
				value = 8388607
			}
			if value < -8388608 {
				value = -8388608
			}
			offset := i * 3
			plane[offset] = byte(value)
			plane[offset+1] = byte(value >> 8)
			plane[offset+2] = byte(value >> 16)
		}
	case media.SampleFormatS32:
		for i, val := range pcm {
			if val > 1.0 {
				val = 1.0
			} else if val < -1.0 {
				val = -1.0
			}
			value := int64(val * 2147483648.0)
			if value > 2147483647 {
				value = 2147483647
			}
			if value < -2147483648 {
				value = -2147483648
			}
			binary.LittleEndian.PutUint32(plane[i*4:(i+1)*4], uint32(int32(value)))
		}
	default:
		return nil, fmt.Errorf("unsupported format for creation: %v", attrs.Format)
	}

	var frame media.Frame = f
	return &frame, nil
}

// PCMGeneratorNode is a pipeline node that generates audio frames from a float32 slice.
type PCMGeneratorNode struct {
	out   *node.OutPort[media.Frame]
	pcm   []float32
	attrs media.AudioAttributes
}

func NewPCMGeneratorNode(pcm []float32, attrs media.AudioAttributes) *PCMGeneratorNode {
	return &PCMGeneratorNode{
		out:   node.NewOutPort[media.Frame]("out", media.StreamInfo{}),
		pcm:   pcm,
		attrs: attrs,
	}
}

func (n *PCMGeneratorNode) Start(ctx context.Context) error {
	outEdge := n.out.Edge()
	if outEdge == nil {
		return fmt.Errorf("generator output not connected")
	}
	defer outEdge.Close()

	frame, err := CreateAudioFrame(n.pcm, n.attrs)
	if err != nil {
		return err
	}

	if err := outEdge.Push(ctx, *frame); err != nil {
		return err
	}

	return nil
}

func (n *PCMGeneratorNode) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"out": n.out}
}
