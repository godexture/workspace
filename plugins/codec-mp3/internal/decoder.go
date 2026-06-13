package internal

import (
	"errors"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
	mp3lib "github.com/hajimehoshi/go-mp3"
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
	dec, err := mp3lib.NewDecoder(d.pr)
	if err != nil {
		d.err = err
		return
	}

	// 1フレームあたりの最大サンプル数 (MPEG1: 1152) * 2(16bit) * 2(stereo) = 4608
	// 余裕を持って少し大きめに確保する
	pcmBuf := make([]byte, 8192)
	for {
		n, err := dec.Read(pcmBuf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, pcmBuf[:n])

			const bytesPerSamplePerChannel = 4 // 2 bytes (S16) * 2 channels
			samples := n / bytesPerSamplePerChannel

			if samples > 0 {
				frame := media.NewAudioFrame(
					media.SampleFormatS16,
					media.LayoutStereo2_0,
					dec.SampleRate(),
					samples,
				)
				copy(frame.Planes()[0], data)
				var f media.Frame = frame
				d.outQueue <- &f
			}
		}

		if err != nil {
			if err != io.EOF {
				d.err = err
			} else {
				d.err = engine.ErrEOF
			}
			return
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
