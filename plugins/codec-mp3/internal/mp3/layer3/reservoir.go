package layer3

import (
	"github.com/godexture/codec-mp3/internal/mp3/bits"
	"github.com/godexture/codec-mp3/internal/mp3/domain"
)

func restoreReservoir(decoder *Decoder, bitReader *bits.BitReader, workspace *Workspace, mainDataOffset int) error {
	remainingFrameBytes := int((bitReader.Limit - bitReader.Position) / 8)
	availableReservoirBytes := min(decoder.bitReservoirBytes, mainDataOffset)

	reservoirStartIndex := decoder.bitReservoirBytes - mainDataOffset
	if reservoirStartIndex < 0 {
		reservoirStartIndex = 0
	}
	copy(workspace.mainData[:], decoder.reservoirBuffer[reservoirStartIndex:reservoirStartIndex+availableReservoirBytes])

	copy(workspace.mainData[availableReservoirBytes:], bitReader.Buffer[int(bitReader.Position/8):int(bitReader.Position/8)+remainingFrameBytes])

	workspace.bitReader.Buffer = workspace.mainData[:]
	workspace.bitReader.Position = 0
	workspace.bitReader.Limit = int32((availableReservoirBytes + remainingFrameBytes) * 8)

	if decoder.bitReservoirBytes < mainDataOffset {
		return domain.ErrInsufficientReservoir
	}
	return nil
}

func saveReservoir(decoder *Decoder, workspace *Workspace) {
	bufferPosition := int((workspace.bitReader.Position + 7) / 8)
	remainingBytes := int(workspace.bitReader.Limit/8) - bufferPosition
	if remainingBytes > maxBitReservoirBytes {
		bufferPosition += remainingBytes - maxBitReservoirBytes
		remainingBytes = maxBitReservoirBytes
	}
	if remainingBytes > 0 {
		copy(decoder.reservoirBuffer[:remainingBytes], workspace.mainData[bufferPosition:bufferPosition+remainingBytes])
	}
	decoder.bitReservoirBytes = remainingBytes
}
