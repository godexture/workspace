package media

import (
	"sync"
	"testing"
)

func TestPooledPacketResetsState(t *testing.T) {
	packet := NewPacket(32, WithStreamIndex(7), WithPts(11), WithDts(13))
	packet.MediaType = MediaAudio
	packet.Kind = PacketKindStreamEnd
	packet.CodecParameters = []CodecParameters{{Data: []byte("metadata")}}
	packet.Release()

	next := NewPacket(8)
	defer next.Release()
	if len(next.Data()) != 8 {
		t.Fatalf("packet data length = %d, want 8", len(next.Data()))
	}
	if next.MediaType != "" || next.StreamIndex != 0 || next.Kind != PacketKindData ||
		next.PTS != 0 || next.DTS != 0 || next.CodecParameters != nil {
		t.Fatalf("pooled packet retained state: %+v", next)
	}
}

func TestPooledPacketHonorsRetain(t *testing.T) {
	packet := NewPacket(4)
	packet.Retain()
	packet.Release()
	copy(packet.Data(), []byte{1, 2, 3, 4})
	if got := packet.Data(); got[0] != 1 || got[3] != 4 {
		t.Fatalf("packet data changed while retained: %v", got)
	}
	packet.Release()
}

func TestPooledPacketConcurrentUse(t *testing.T) {
	const (
		workers    = 16
		iterations = 1000
	)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := range workers {
		go func() {
			defer group.Done()
			for iteration := range iterations {
				packet := NewPacket(64, WithStreamIndex(worker), WithPts(Pts(iteration)))
				data := packet.Data()
				data[0] = byte(worker)
				data[len(data)-1] = byte(iteration)
				if packet.StreamIndex != worker || packet.PTS != Pts(iteration) ||
					data[0] != byte(worker) || data[len(data)-1] != byte(iteration) {
					t.Errorf("packet state corrupted for worker %d iteration %d", worker, iteration)
				}
				packet.Release()
			}
		}()
	}
	group.Wait()
}
