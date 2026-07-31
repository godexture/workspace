package pipeline

import (
	"context"
	"math/big"
	"time"

	"github.com/godexture/godec/core/domain/media"
)

type observedEdge[T any] struct {
	*ChanEdge[T]
	metrics *edgeMetrics
}

type progressEdge[T any] struct {
	*ChanEdge[T]
	metrics *edgeMetrics
}

func (e *progressEdge[T]) Push(ctx context.Context, item T) error {
	mediaTime := measureItemTime(item)
	if err := e.ChanEdge.Push(ctx, item); err != nil {
		return err
	}
	e.metrics.items.Add(1)
	e.metrics.updateMediaTime(mediaTime)
	return nil
}

func (e *observedEdge[T]) Push(ctx context.Context, item T) error {
	bytes, samples, mediaTime := measureItem(item)
	if err := e.ChanEdge.Push(ctx, item); err != nil {
		return err
	}
	e.metrics.items.Add(1)
	e.metrics.bytes.Add(bytes)
	e.metrics.samples.Add(samples)
	e.metrics.updateMediaTime(mediaTime)
	return nil
}

func measureItemTime[T any](item T) time.Duration {
	switch value := any(item).(type) {
	case *media.Packet:
		if value != nil {
			return packetTime(value)
		}
	case media.Frame:
		if audio, ok := value.(*media.AudioFrame); ok && audio != nil && audio.SampleRate > 0 {
			end := int64(audio.Pts()) + int64(audio.Samples)
			if end > 0 {
				return time.Duration(end) * time.Second / time.Duration(audio.SampleRate)
			}
		}
	}
	return 0
}

func measureItem[T any](item T) (bytes uint64, samples uint64, mediaTime time.Duration) {
	switch value := any(item).(type) {
	case *media.Packet:
		if value == nil {
			return 0, 0, 0
		}
		if value.Kind == media.PacketKindData {
			bytes = uint64(len(value.Data()))
		}
		mediaTime = packetTime(value)
	case media.Frame:
		if audio, ok := value.(*media.AudioFrame); ok && audio != nil {
			for _, plane := range audio.Planes() {
				bytes += uint64(len(plane))
			}
			if audio.Samples > 0 {
				samples = uint64(audio.Samples)
			}
			if audio.SampleRate > 0 {
				end := int64(audio.Pts()) + int64(audio.Samples)
				if end > 0 {
					mediaTime = time.Duration(end) * time.Second / time.Duration(audio.SampleRate)
				}
			}
		}
	}
	return bytes, samples, mediaTime
}

func packetTime(packet *media.Packet) time.Duration {
	if packet.PTS <= 0 {
		return 0
	}
	rational := (*big.Rat)(&packet.Timebase)
	numerator := rational.Num().Int64()
	denominator := rational.Denom().Int64()
	if numerator <= 0 || denominator <= 0 {
		return 0
	}
	return time.Duration(float64(packet.PTS) * float64(numerator) / float64(denominator) * float64(time.Second))
}
