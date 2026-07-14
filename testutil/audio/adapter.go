package audio

import (
	"bytes"
	"errors"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

// EngineDemux creates a DemuxFunc using the given DemuxerEngine factory.
func EngineDemux(factory func(io.ReadSeeker) (engine.DemuxerEngine, error)) DemuxFunc {
	return func(data []byte) ([][]byte, error) {
		demuxer, err := factory(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		var packets [][]byte
		for {
			pkt, _, err := demuxer.ReadPacket()
			if err != nil {
				if errors.Is(err, engine.ErrEOF) || errors.Is(err, io.EOF) || errors.Is(err, engine.ErrEAGAIN) {
					break
				}
				return nil, err
			}
			pdata := make([]byte, len(pkt.Data()))
			copy(pdata, pkt.Data())
			packets = append(packets, pdata)
		}
		return packets, nil
	}
}

// EngineDecode creates a DecodeFunc using the given DecoderEngine factory.
func EngineDecode(streamInfo media.StreamInfo, factory func(media.StreamInfo) engine.DecoderEngine) DecodeFunc {
	return func(packets [][]byte) ([]float32, error) {
		decoder := factory(streamInfo)

		var pcm []float32
		for _, p := range packets {
			pkt := media.NewPacket(len(p))
			copy(pkt.Data(), p)
			if err := decoder.SendPacket(pkt); err != nil {
				return nil, err
			}
			for {
				frame, err := decoder.ReceiveFrame()
				if err != nil {
					if errors.Is(err, engine.ErrEAGAIN) || errors.Is(err, engine.ErrEOF) || errors.Is(err, io.EOF) {
						break
					}
					return nil, err
				}
				af := (*frame).(*media.AudioFrame)
				pcmSlice, _ := ConvertToFloat32(af)
				pcm = append(pcm, pcmSlice...)
			}
		}
		if err := decoder.Flush(); err != nil {
			return nil, err
		}
		for {
			frame, err := decoder.ReceiveFrame()
			if err != nil {
				if errors.Is(err, engine.ErrEAGAIN) || errors.Is(err, engine.ErrEOF) || errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			af := (*frame).(*media.AudioFrame)
			pcmSlice, _ := ConvertToFloat32(af)
			pcm = append(pcm, pcmSlice...)
		}
		return pcm, nil
	}
}

// EngineEncode creates an EncodeFunc using the given EncoderEngine factory and AudioAttributes.
func EngineEncode(attrs media.AudioAttributes, factory func() engine.EncoderEngine) EncodeFunc {
	return func(pcm []float32) ([][]byte, error) {
		encoder := factory()

		frame, err := CreateAudioFrame(pcm, attrs)
		if err != nil {
			return nil, err
		}

		if err := encoder.SendFrame(frame); err != nil {
			return nil, err
		}

		if err := encoder.Flush(); err != nil {
			return nil, err
		}

		var packets [][]byte
		for {
			pkt, err := encoder.ReceivePacket()
			if err != nil {
				if errors.Is(err, engine.ErrEAGAIN) || errors.Is(err, engine.ErrEOF) || errors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}
			pdata := make([]byte, len(pkt.Data()))
			copy(pdata, pkt.Data())
			packets = append(packets, pdata)
		}
		return packets, nil
	}
}

// EngineMux creates a MuxFunc using the given MuxerEngine factory and StreamInfo.
func EngineMux(streamInfo media.StreamInfo, factory func(*Buffer) engine.MuxerEngine) MuxFunc {
	return func(packets [][]byte) ([]byte, error) {
		buf := NewBuffer(nil)
		muxer := factory(buf)
		idx, err := muxer.AddStream(streamInfo)
		if err != nil {
			return nil, err
		}

		if err := muxer.WriteHeader(); err != nil {
			return nil, err
		}

		for _, p := range packets {
			pkt := media.NewPacket(len(p))
			copy(pkt.Data(), p)
			if err := muxer.WritePacket(idx, pkt); err != nil {
				return nil, err
			}
		}

		if err := muxer.WriteTrailer(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
}
