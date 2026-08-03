package decoder

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/flac/internal/codec/config"
	"github.com/godexture/godec/plugin/flac/internal/streaminfo"
	"github.com/godexture/godec/sdk/engine"
)

// BenchmarkParallelDecodeThroughput exercises the exact OutputReady/markReady
// path this benchmark exists to protect: a real WorkerPool with parallelism
// > 1, many packets in flight at once, and a single consumer goroutine
// draining ReceiveFrame as fast as it can -- the scenario where the shared
// waitCh mutex could show up as contention against concurrent markReady
// calls from pool workers.
func BenchmarkParallelDecodeThroughput(b *testing.B) {
	data, err := hex.DecodeString("664c6143800000221000100000000f00000f0ac442f0000000013e84b41807dc690307586a3dad1a2e0ffff869180000bf0358fd03128baa9a")
	if err != nil {
		b.Fatal(err)
	}
	stream := media.StreamInfo{}
	stream.Metadata = *metadata.NewBundle()
	stream.Metadata.AddRaw(streaminfo.MetadataKey, data[8:42])
	frameData := data[42:]

	for _, parallelism := range []int{1, 4, 16} {
		b.Run(parallelismName(parallelism), func(b *testing.B) {
			pool := registry.NewWorkerPool(parallelism)
			defer pool.Close()
			const packets = 64
			// Packets are pre-built once and reused across iterations: SendPacket
			// only reads pkt.Data() (copying it into a pooled buffer), never
			// mutates or releases it, and every iteration's decoder fully drains
			// (Flush + ReceiveFrame to EOF) before the packets are reused again.
			pkts := make([]*media.Packet, packets)
			for i := range pkts {
				pkts[i] = media.NewPacketFromData(append([]byte(nil), frameData...))
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				decoder := NewDecoder(stream, config.DefaultDecoderConfig, pool)
				for _, pkt := range pkts {
					if err := decoder.SendPacket(pkt); err != nil {
						b.Fatal(err)
					}
				}
				if err := decoder.Flush(); err != nil {
					b.Fatal(err)
				}
				got := 0
				for {
					frame, err := decoder.ReceiveFrame()
					if err == engine.ErrEOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
					frame.Release()
					got++
				}
				if got != packets {
					b.Fatalf("got %d frames, want %d", got, packets)
				}
			}
		})
	}
}

func parallelismName(n int) string {
	return fmt.Sprintf("Parallelism=%d", n)
}
