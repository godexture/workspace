package access

import (
	"errors"

	"github.com/godexture/godec/media/buffer"
)

// WriteOperation identifies how a write payload is positioned at a sink.
type WriteOperation uint8

const (
	AppendOperation WriteOperation = iota + 1
	PatchOperation
)

func (o WriteOperation) Valid() bool { return o == AppendOperation || o == PatchOperation }

var ErrInvalidWrite = errors.New("access write is invalid")

// Write owns one payload to append or patch at an absolute byte offset.
type Write struct {
	operation WriteOperation
	offset    int64
	payload   buffer.Handle
}

// Append and Patch take ownership of payload. A rejected write releases it, so
// converting an item into a positioned write never strands a payload on the
// failure path.
func Append(payload buffer.Handle) (Write, error) {
	return newWrite(AppendOperation, 0, payload)
}

func Patch(offset int64, payload buffer.Handle) (Write, error) {
	return newWrite(PatchOperation, offset, payload)
}

func newWrite(operation WriteOperation, offset int64, payload buffer.Handle) (Write, error) {
	if !operation.Valid() || offset < 0 || !payload.Valid() {
		payload.Release()
		return Write{}, ErrInvalidWrite
	}
	return Write{operation: operation, offset: offset, payload: payload}, nil
}

func (w Write) Valid() bool {
	return w.operation.Valid() && w.offset >= 0 && w.payload.Valid()
}

func (w Write) Operation() WriteOperation { return w.operation }
func (w Write) Offset() int64             { return w.offset }
func (w Write) Payload() buffer.View      { return w.payload.Borrow() }
func (w Write) Bytes() []byte             { return w.payload.Bytes() }

func (w Write) Share() Write {
	w.payload = w.payload.Share()
	return w
}

func (w Write) Release() { w.payload.Release() }
