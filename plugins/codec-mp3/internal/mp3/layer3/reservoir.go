package layer3

import (
	"github.com/godexture/codec-mp3/internal/mp3/domain"
	"github.com/godexture/sdk/bits"
)

func restoreReservoir(decoder *Decoder, bitReader *bits.Reader, workspace *Workspace, mainDataOffset int) error {
	unread := bitReader.Unread()
	availableReservoirBytes := min(decoder.bitReservoirBytes, mainDataOffset)

	reservoirStartIndex := decoder.bitReservoirBytes - mainDataOffset
	if reservoirStartIndex < 0 {
		reservoirStartIndex = 0
	}
	copy(workspace.mainData[:], decoder.reservoirBuffer[reservoirStartIndex:reservoirStartIndex+availableReservoirBytes])

	copy(workspace.mainData[availableReservoirBytes:], unread)

	workspace.bitReader.Init(workspace.mainData[:], 0, int32((availableReservoirBytes+len(unread))*8))

	if decoder.bitReservoirBytes < mainDataOffset {
		return domain.ErrInsufficientReservoir
	}
	return nil
}

func saveReservoir(decoder *Decoder, workspace *Workspace) {
	unread := workspace.bitReader.Unread()
	if len(unread) > maxBitReservoirBytes {
		unread = unread[len(unread)-maxBitReservoirBytes:]
	}
	if len(unread) > 0 {
		copy(decoder.reservoirBuffer[:len(unread)], unread)
	}
	decoder.bitReservoirBytes = len(unread)
}
