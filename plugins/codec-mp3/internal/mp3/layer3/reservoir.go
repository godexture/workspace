package layer3

import (
	"github.com/godexture/codec-mp3/internal/mp3/domain"
	"github.com/godexture/sdk/bits"
)

func restoreReservoir(decoder *Decoder, bitReader *bits.Reader, workspace *Workspace, mainDataOffset int) error {
	unread := bitReader.Unread()
	reservoirBytes := decoder.reservoir.Len()
	decoder.reservoir.Append(unread)

	start := reservoirBytes - mainDataOffset
	if start < 0 {
		start = 0
	}
	data := decoder.reservoir.Data()
	workspace.bitReader.Init(data, int32(start*8), int32(len(data)*8))

	if reservoirBytes < mainDataOffset {
		return domain.ErrInsufficientReservoir
	}
	return nil
}

func saveReservoir(decoder *Decoder, workspace *Workspace) {
	unreadBytes := len(workspace.bitReader.Unread())
	if unreadBytes > maxBitReservoirBytes {
		unreadBytes = maxBitReservoirBytes
	}
	decoder.reservoir.Discard(decoder.reservoir.Len() - unreadBytes)
}
