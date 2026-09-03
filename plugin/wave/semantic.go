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
		return left.BlockCount() == 0 && right.BlockCount() == 0
	}
	if left.Scope() != right.Scope() || left.Len() != right.Len() {
		return false
	}
	if left.BlockCount() != 0 || right.BlockCount() != 0 {
		return false
	}
	for index := 0; index < left.Len(); index++ {
		leftEntry, leftOK := left.EntryAt(index)
		rightEntry, rightOK := right.EntryAt(index)
		if !leftOK || !rightOK || leftEntry.Key() != rightEntry.Key() || !reflect.DeepEqual(leftEntry.Value(), rightEntry.Value()) {
			return false
		}
	}
	return true
}
