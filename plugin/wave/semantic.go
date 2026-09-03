package wave

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/media/metadata"
)

func waveSemanticDocument(value metadata.Attachment) (metadata.Document, error) {
	if err := validateWaveMetadataAttachment(value); err != nil {
		return metadata.Document{}, err
	}
	document, err := value.Semantic()
	if errors.Is(err, metadata.ErrMetadataAbsent) {
		return metadata.Document{}, nil
	}
	return document, err
}

func waveMetadataAttachmentValid(value metadata.Attachment) bool {
	return validateWaveMetadataAttachment(value) == nil
}

func validateWaveMetadataAttachment(value metadata.Attachment) error {
	if !value.Valid() {
		return fmt.Errorf("%w: WAVE metadata attachment is invalid", ErrUnsupported)
	}
	if !value.IsAbsent() && value.Scope() != metadata.AssetScope {
		return fmt.Errorf("%w: WAVE metadata attachment must use AssetScope", ErrUnsupported)
	}
	return nil
}

func (h header) metadataAttachment() metadata.Attachment {
	return h.metadata
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
