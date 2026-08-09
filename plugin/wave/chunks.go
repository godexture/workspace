package wave

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
)

type rawChunkSlot struct{}

type chunkAnchor uint8

const (
	chunkBeforeFormat chunkAnchor = iota + 1
	chunkBeforeData
	chunkAfterData
)

type chunkKind uint8

const (
	chunkRaw chunkKind = iota + 1
	chunkInfo
)

type chunkBlock struct {
	position uint64
	anchor   chunkAnchor
	kind     chunkKind
}

func rawChunkCarrier() carrier.ID { return carrier.Define[rawChunkSlot]() }

func newChunkBlockID(position uint64, anchor chunkAnchor, kind chunkKind) metadata.BlockID {
	return metadata.BlockID(fmt.Sprintf("wave/chunk/%016x/%s/%s", position, anchor.token(), kind.token()))
}

func parseChunkBlockID(value metadata.BlockID) (chunkBlock, bool) {
	parts := strings.Split(string(value), "/")
	if len(parts) != 5 || parts[0] != "wave" || parts[1] != "chunk" {
		return chunkBlock{}, false
	}
	position, err := strconv.ParseUint(parts[2], 16, 64)
	if err != nil {
		return chunkBlock{}, false
	}
	anchor, ok := parseChunkAnchor(parts[3])
	if !ok {
		return chunkBlock{}, false
	}
	kind, ok := parseChunkKind(parts[4])
	if !ok {
		return chunkBlock{}, false
	}
	return chunkBlock{position: position, anchor: anchor, kind: kind}, true
}

func chunkAnchorAt(formatFound, dataFound bool) chunkAnchor {
	if dataFound {
		return chunkAfterData
	}
	if formatFound {
		return chunkBeforeData
	}
	return chunkBeforeFormat
}

func (a chunkAnchor) token() string {
	switch a {
	case chunkBeforeFormat:
		return "before-format"
	case chunkBeforeData:
		return "before-data"
	case chunkAfterData:
		return "after-data"
	default:
		return "invalid"
	}
}

func parseChunkAnchor(value string) (chunkAnchor, bool) {
	switch value {
	case "before-format":
		return chunkBeforeFormat, true
	case "before-data":
		return chunkBeforeData, true
	case "after-data":
		return chunkAfterData, true
	default:
		return 0, false
	}
}

func (k chunkKind) token() string {
	switch k {
	case chunkRaw:
		return "raw"
	case chunkInfo:
		return "info"
	default:
		return "invalid"
	}
}

func parseChunkKind(value string) (chunkKind, bool) {
	switch value {
	case "raw":
		return chunkRaw, true
	case "info":
		return chunkInfo, true
	default:
		return 0, false
	}
}
