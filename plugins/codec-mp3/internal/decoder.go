package internal

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/godexture/codec-mp3/internal/mp3"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// DecoderConfig はMP3デコーダの設定。
type DecoderConfig struct{}

func (DecoderConfig) NodeConfigaration() {}

type Decoder struct {
	buf        bytes.Buffer
	dec        mp3.Mp3Dec
	floatPcm   []float32
	intPcm     []int16
	sampleRate int
	channels   int
	id3ToSkip  int
	flushed    bool
	err        error
}

func NewDecoder() *Decoder {
	d := &Decoder{
		id3ToSkip: -1,
		floatPcm:  make([]float32, 1152*2),
		intPcm:    make([]int16, 1152*2),
	}
	d.dec.Init()
	return d
}

// processFrame processes raw PCM float samples, converts them to S16 format,
// and packages them into a media.Frame.
func processFrame(floatPcm []float32, intPcm []int16, samples int, info mp3.Mp3DecFrameInfo) (media.Frame, error) {
	channels := info.Channels
	decodedSamples := samples * channels
	mp3.FloatToS16(floatPcm[:decodedSamples], intPcm[:decodedSamples])

	var layout media.ChannelLayout
	if channels == 1 {
		layout = media.LayoutMono1
	} else {
		layout = media.LayoutStereo2_0
	}

	frame := media.NewAudioFrame(
		media.SampleFormatS16,
		layout,
		info.Hz,
		samples,
	)

	plane := frame.Planes()[0]
	for i := 0; i < decodedSamples; i++ {
		binary.LittleEndian.PutUint16(plane[i*2:], uint16(intPcm[i]))
	}
	return frame, nil
}

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("codec-mp3 decoder: received nil packet")
	}
	if d.flushed {
		return engine.ErrEOF
	}
	if d.err != nil {
		return d.err
	}

	d.buf.Write(pkt.Data())
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	if d.err != nil {
		return nil, d.err
	}

	for {
		// 1. Skip ID3 tags on the first frame
		if d.id3ToSkip == -1 {
			bufBytes := d.buf.Bytes()
			if len(bufBytes) >= 3 && (bufBytes[0] != 'I' || bufBytes[1] != 'D' || bufBytes[2] != '3') {
				d.id3ToSkip = 0
			} else if len(bufBytes) >= 10 {
				if size, ok := mp3.ParseId3v2Size(bufBytes); ok {
					d.id3ToSkip = size
				} else {
					d.id3ToSkip = 0
				}
			} else if d.flushed {
				d.id3ToSkip = 0
			} else {
				return nil, engine.ErrEAGAIN
			}
		}

		if d.id3ToSkip > 0 {
			toSkip := d.id3ToSkip
			if toSkip > d.buf.Len() {
				toSkip = d.buf.Len()
			}
			d.buf.Next(toSkip)
			d.id3ToSkip -= toSkip
			if d.id3ToSkip > 0 {
				if d.flushed {
					return nil, engine.ErrEOF
				}
				return nil, engine.ErrEAGAIN
			}
		}

		// 2. Terminate if buffer is empty
		if d.buf.Len() == 0 {
			if d.flushed {
				return nil, engine.ErrEOF
			}
			return nil, engine.ErrEAGAIN
		}

		// If we don't have enough bytes to read a 4-byte MP3 header, wait for more data.
		if d.buf.Len() < 4 && !d.flushed {
			return nil, engine.ErrEAGAIN
		}

		// 3. Try to decode a frame
		samples, info := d.dec.DecodeFrame(d.buf.Bytes(), d.floatPcm)
		if info.FrameBytes > 0 {
			d.buf.Next(info.FrameBytes)

			if samples > 0 {
				if d.sampleRate == 0 {
					d.sampleRate = info.Hz
					d.channels = info.Channels
				} else if info.Hz != d.sampleRate || info.Channels != d.channels {
					d.err = errors.New("codec-mp3 decoder: sample rate or channels changed mid-stream")
					return nil, d.err
				}

				frame, err := processFrame(d.floatPcm, d.intPcm, samples, info)
				if err != nil {
					d.err = err
					return nil, d.err
				}
				return &frame, nil
			}

			// If samples == 0, it means it's a headers-only frame or invalid frame.
			// Try decoding next frame.
			continue
		} else {
			// info.FrameBytes == 0 means not enough data to confirm/decode a frame.
			if d.flushed {
				if d.buf.Len() > 0 {
					d.buf.Reset() // Discard remaining junk bytes
				}
				return nil, engine.ErrEOF
			}

			// If the buffer grows too large without finding a frame (garbage data),
			// discard a byte to prevent memory leaks and try again.
			if d.buf.Len() > 64*1024 {
				d.buf.Next(1)
				continue
			}

			return nil, engine.ErrEAGAIN
		}
	}
}

func (d *Decoder) Flush() error {
	d.flushed = true
	return nil
}
