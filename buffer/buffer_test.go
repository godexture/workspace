package buffer

import (
	"bytes"
	"testing"
)

func TestBlockBuffer(t *testing.T) {
	t.Run("empty buffer", func(t *testing.T) {
		buf := &BlockBuffer{}
		res := buf.TakeBlocks(10)
		if res != nil {
			t.Errorf("expected nil, got %v", res)
		}
	})

	t.Run("not enough data", func(t *testing.T) {
		buf := &BlockBuffer{}
		buf.Append([]byte{1, 2, 3})
		res := buf.TakeBlocks(5)
		if res != nil {
			t.Errorf("expected nil, got %v", res)
		}
	})

	t.Run("exact block size", func(t *testing.T) {
		buf := &BlockBuffer{}
		buf.Append([]byte{1, 2, 3, 4, 5})
		res := buf.TakeBlocks(5)
		if !bytes.Equal(res, []byte{1, 2, 3, 4, 5}) {
			t.Errorf("unexpected result: %v", res)
		}
		if len(buf.buf) != 0 {
			t.Errorf("expected empty buffer, got %v", buf.buf)
		}
	})

	t.Run("more than block size", func(t *testing.T) {
		buf := &BlockBuffer{}
		buf.Append([]byte{1, 2, 3, 4, 5, 6, 7})
		res := buf.TakeBlocks(5)
		if !bytes.Equal(res, []byte{1, 2, 3, 4, 5}) {
			t.Errorf("unexpected result: %v", res)
		}
		if !bytes.Equal(buf.buf, []byte{6, 7}) {
			t.Errorf("unexpected remaining buffer: %v", buf.buf)
		}
	})

	t.Run("take all", func(t *testing.T) {
		buf := &BlockBuffer{}
		buf.Append([]byte{1, 2, 3})
		res := buf.TakeAll()
		if !bytes.Equal(res, []byte{1, 2, 3}) {
			t.Errorf("unexpected TakeAll: %v", res)
		}
		if buf.buf != nil {
			t.Errorf("expected nil buffer after TakeAll, got %v", buf.buf)
		}
	})
}
