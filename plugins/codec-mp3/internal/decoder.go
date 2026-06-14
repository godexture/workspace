package internal

import (
	"encoding/binary"
	"errors"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// DecoderConfig はMP3デコーダの設定。
type DecoderConfig struct{}

func (DecoderConfig) NodeConfigaration() {}

type Decoder struct {
	inQueue  chan *media.Packet
	outQueue chan *media.Frame
	pw       *io.PipeWriter
	pr       *io.PipeReader
	err      error
	flushed  bool
}

func NewDecoder() *Decoder {
	pr, pw := io.Pipe()
	d := &Decoder{
		inQueue:  make(chan *media.Packet, 100),
		outQueue: make(chan *media.Frame, 100),
		pw:       pw,
		pr:       pr,
	}

	go d.writeLoop()
	go d.decodeLoop()

	return d
}

func (d *Decoder) writeLoop() {
	for pkt := range d.inQueue {
		_, err := d.pw.Write(pkt.Data())
		if err != nil {
			return
		}
	}
	d.pw.Close()
}

func (d *Decoder) decodeLoop() {
	defer close(d.outQueue)

	var dec Mp3Dec
	dec.Init()

	buf := make([]byte, 32*1024)
	bufLen := 0
	eof := false

	floatPcm := make([]float32, 1152*2)
	intPcm := make([]int16, 1152*2)

	var sampleRate int
	var channels int
	firstFrame := true

	for {
		// 1. Read from reader if not EOF and buffer has space
		if !eof && bufLen < len(buf) {
			n, err := d.pr.Read(buf[bufLen:])
			if n > 0 {
				bufLen += n
			}
			if err != nil {
				eof = true
				if err != io.EOF {
					d.err = err
				} else {
					d.err = engine.ErrEOF
				}
			}
		}

		// 2. Skip ID3 tags on the first iteration
		if firstFrame && bufLen > 0 {
			skipped := SkipId3(buf[:bufLen])
			if skipped > 0 {
				if skipped < bufLen {
					copy(buf, buf[skipped:bufLen])
					bufLen -= skipped
				} else {
					bufLen = 0
				}
			}
			firstFrame = false
		}

		// 3. Terminate if buffer is empty and we hit EOF
		if bufLen == 0 && eof {
			return
		}

		// 4. Try to decode a frame
		if bufLen == 0 {
			continue
		}

		samples, info := dec.DecodeFrame(buf[:bufLen], floatPcm)
		if info.FrameBytes > 0 {
			if samples > 0 {
				sampleRate = info.Hz
				channels = info.Channels

				decodedSamples := samples * channels
				FloatToS16(floatPcm[:decodedSamples], intPcm[:decodedSamples])

				byteLen := decodedSamples * 2
				byteBuf := make([]byte, byteLen)
				for i := 0; i < decodedSamples; i++ {
					binary.LittleEndian.PutUint16(byteBuf[i*2:], uint16(intPcm[i]))
				}

				var layout media.ChannelLayout
				if channels == 1 {
					layout = media.LayoutMono1
				} else {
					layout = media.LayoutStereo2_0
				}

				frame := media.NewAudioFrame(
					media.SampleFormatS16,
					layout,
					sampleRate,
					samples,
				)
				copy(frame.Planes()[0], byteBuf)
				var f media.Frame = frame
				d.outQueue <- &f
			}

			// Consume frame bytes
			if info.FrameBytes < bufLen {
				copy(buf, buf[info.FrameBytes:bufLen])
				bufLen -= info.FrameBytes
			} else {
				bufLen = 0
			}
		} else {
			if eof {
				return
			}
			if bufLen == len(buf) {
				copy(buf, buf[1:])
				bufLen--
			}
		}
	}
}

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("codec-mp3 decoder: received nil packet")
	}
	if d.flushed {
		return engine.ErrEOF
	}

	d.inQueue <- pkt
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	select {
	case f, ok := <-d.outQueue:
		if !ok {
			if d.err != nil {
				return nil, d.err
			}
			return nil, engine.ErrEOF
		}
		return f, nil
	default:
		// flushed かつ inQueue が空なら待機するべきか？
		// writeLoop が全てを pw に書き込み、decodeLoop が全てを outQueue に書き込むまで
		// 少しタイムラグがある。
		// runCodecLoop は flush() 後も receive() をループで呼ぶため
		// EAGAIN を返して良いが、完全に終了した場合は EOF を返す必要がある。
		if d.flushed && len(d.inQueue) == 0 {
			// まだ decodeLoop が終了していない場合は待つために EAGAIN を返す
			select {
			case <-d.outQueue: // 閉じているか確認 (上で select しているので不完全だが)
			default:
				// no-op
			}
		}
		return nil, engine.ErrEAGAIN
	}
}

func (d *Decoder) Flush() error {
	if !d.flushed {
		d.flushed = true
		close(d.inQueue)
	}
	return nil
}
