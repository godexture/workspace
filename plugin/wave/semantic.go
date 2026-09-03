package wave

import (
	"errors"
	"reflect"

	"github.com/godexture/godec/media/metadata"
)

func waveSemanticDocument(value metadata.Attachment) (metadata.Document, error) {
	document, err := value.Semantic()
	if errors.Is(err, metadata.ErrMetadataAbsent) {
		return metadata.Document{}, nil
	}
	return document, err
}

func (h header) metadataAttachment() metadata.Attachment {
	if h.ranges.infoCount != 0 && h.metadata.Valid() && h.metadata.Scope() == metadata.StreamScope {
		return metadata.MustAvailable(h.metadata)
	}
	return metadata.Absent()
}

func sameSemanticDocument(left, right metadata.Document) bool {
	if left.Len() == 0 && right.Len() == 0 {
		return len(left.Blocks()) == 0 && len(right.Blocks()) == 0
	}
	if left.Scope() != right.Scope() || left.Len() != right.Len() {
		return false
	}
	leftEntries, rightEntries := left.Entries(), right.Entries()
	if len(leftEntries) != len(rightEntries) || len(left.Blocks()) != 0 || len(right.Blocks()) != 0 {
		return false
	}
	for index := range leftEntries {
		if leftEntries[index].Key() != rightEntries[index].Key() || !reflect.DeepEqual(leftEntries[index].Value(), rightEntries[index].Value()) {
			return false
		}
	}
	return true
}

func semanticWithinCap(document metadata.Document, cap uint64) bool {
	if uint64(len(document.Entries())) > cap {
		return false
	}
	used := uint64(0)
	for _, entry := range document.Entries() {
		value := entry.Value()
		switch typed := value.(type) {
		case string:
			if used > cap || uint64(len(typed)) > cap-used {
				return false
			}
			used += uint64(len(typed))
		default:
			// WAVE INFO's supported values are strings and bounded date
			// structs. A small fixed allowance covers the latter without
			// retaining arbitrary payloads.
			if used > cap || 64 > cap-used {
				return false
			}
			used += 64
		}
	}
	return true
}
