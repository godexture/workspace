package internal

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/godexture/godec/core/domain/metadata"
)

func parseLISTChunk(r io.ReadSeeker, chunkSize uint32, listMeta *metadata.Bundle) error {
	if chunkSize < 4 {
		if _, err := r.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
			return err
		}
		return nil
	}
	var listType [4]byte
	if _, err := io.ReadFull(r, listType[:]); err != nil {
		return err
	}
	if string(listType[:]) == wavTagINFO {
		remaining := int64(chunkSize) - 4
		for remaining >= 8 {
			var subID [4]byte
			if _, err := io.ReadFull(r, subID[:]); err != nil {
				return err
			}
			var subSize uint32
			if err := binary.Read(r, binary.LittleEndian, &subSize); err != nil {
				return err
			}
			remaining -= 8

			if int64(subSize) > remaining {
				break
			}

			valBuf := make([]byte, subSize)
			if _, err := io.ReadFull(r, valBuf); err != nil {
				return err
			}
			remaining -= int64(subSize)

			if subSize%2 == 1 {
				if remaining > 0 {
					if _, err := r.Seek(1, io.SeekCurrent); err != nil {
						return err
					}
					remaining--
				}
			}

			valStr := string(bytes.TrimRight(valBuf, "\x00"))
			mapWavInfoTag(listMeta, string(subID[:]), valStr)
		}
		if remaining > 0 {
			if _, err := r.Seek(remaining, io.SeekCurrent); err != nil {
				return err
			}
		}
	} else {
		if _, err := r.Seek(int64(chunkSize)-4, io.SeekCurrent); err != nil {
			return err
		}
	}
	return nil
}
