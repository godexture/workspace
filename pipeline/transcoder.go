package pipeline

// import (
// 	"context"

// 	"github.com/godexture/core/domain/media"
// 	"github.com/godexture/core/domain/node"
// 	"golang.org/x/sync/errgroup"
// )

// type Runner struct {
// 	Demuxer node.Demuxer
// 	Muxer   node.Muxer

// 	Decoder node.Decoder
// 	Encoder node.Encoder

// 	Filters []node.Filter

// 	outStreamIndex int
// }

// func (t *Runner) Run(ctx context.Context) error {
// 	meta := t.Demuxer.Metadata()
// 	if err := t.Muxer.SetMetadata(meta); err != nil {
// 		return err
// 	}

// 	eg, ctx := errgroup.WithContext(ctx)

// 	packetChan := make(chan *media.Packet, 100)
// 	frameChan := make(chan media.Frame, 100)
// 	encodedChan := make(chan *media.Packet, 100)

// 	eg.Go(func() error {
// 		defer close(packetChan)
// 		for {
// 			select {
// 			case <-ctx.Done():
// 				return ctx.Err()
// 			default:
// 				pkt, err := t.Demuxer.ReadPacket()
// 				if err != nil {
// 					return err
// 				}
// 				packetChan <- pkt
// 			}
// 		}
// 	})

// 	eg.Go(func() error {
// 		defer close(frameChan)
// 		for pkt := range packetChan {
// 			frame, err := t.Decoder.Decode(pkt)
// 			if err != nil {
// 				pkt.Release()
// 				return err
// 			}
// 			frameChan <- frame
// 			pkt.Release()
// 		}
// 		return nil
// 	})

// 	var currentFrameChan <-chan media.Frame = frameChan
// 	for _, filter := range t.Filters {
// 		outChan, errChan := filter.Process(ctx, currentFrameChan)
// 		currentFrameChan = outChan

// 		eg.Go(func() error {
// 			for err := range errChan {
// 				return err
// 			}
// 			return nil
// 		})
// 	}

// 	eg.Go(func() error {
// 		defer close(encodedChan)
// 		for frame := range currentFrameChan {
// 			outPkt, err := t.Encoder.Encode(frame)
// 			if err != nil {
// 				frame.Release()
// 				return err
// 			}
// 			encodedChan <- outPkt
// 			frame.Release()
// 		}
// 		return nil
// 	})

// 	eg.Go(func() error {
// 		for pkt := range encodedChan {
// 			err := t.Muxer.WritePacket(t.outStreamIndex, pkt)
// 			pkt.Release()
// 			if err != nil {
// 				return err
// 			}
// 		}
// 		return nil
// 	})

// 	return eg.Wait()
// }
