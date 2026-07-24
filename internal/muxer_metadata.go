package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/format-flac/streaminfo"
)

type metadataBlock struct {
	blockType byte
	payload   []byte
}

func metadataBlocks(meta metadata.Bundle) ([]metadataBlock, error) {
	var blocks []metadataBlock
	for _, raw := range metaRawBlocks(meta) {
		if len(raw) < 4 {
			return nil, errors.New("flac muxer metadata block is shorter than its header")
		}
		blockType := raw[0] & 0x7f
		if blockType == streaminfo.MetadataTypeStreamInfo || blockType > 6 {
			return nil, fmt.Errorf("flac muxer cannot write metadata block type %d", blockType)
		}
		length := int(raw[1])<<16 | int(raw[2])<<8 | int(raw[3])
		if length != len(raw)-4 {
			return nil, fmt.Errorf("flac muxer metadata block length mismatch: header=%d payload=%d", length, len(raw)-4)
		}
		blocks = append(blocks, metadataBlock{blockType: blockType, payload: raw[4:]})
	}
	return blocks, nil
}

func metaRawBlocks(meta metadata.Bundle) [][]byte {
	raw, _ := meta.GetRaw(streaminfo.MetadataBlockKey)
	return raw
}

func writeMetadataBlockHeader(w io.Writer, last bool, blockType byte, length int) error {
	header := [4]byte{blockType, byte(length >> 16), byte(length >> 8), byte(length)}
	if last {
		header[0] |= 0x80
	}
	return writeAll(w, header[:])
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
