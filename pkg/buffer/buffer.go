package buffer

// BlockBuffer manages bytes buffering for block-based encoders/decoders.
type BlockBuffer struct {
	buf []byte
}

// Append adds data to the buffer.
func (b *BlockBuffer) Append(data []byte) {
	if len(data) == 0 {
		return
	}
	b.buf = append(b.buf, data...)
}

// TakeBlocks returns data corresponding to the maximum possible number of blocks
// of size bytesPerBlock. The returned data is sliced from the buffer, and the
// remaining data is retained. Returns nil if there is not enough data for at least one block.
func (b *BlockBuffer) TakeBlocks(bytesPerBlock int) []byte {
	if bytesPerBlock <= 0 {
		return nil
	}
	numBlocks := len(b.buf) / bytesPerBlock
	if numBlocks == 0 {
		return nil
	}
	takeSize := numBlocks * bytesPerBlock
	toEncode := b.buf[:takeSize]

	remaining := len(b.buf) - takeSize
	if remaining > 0 {
		newBuf := make([]byte, remaining)
		copy(newBuf, b.buf[takeSize:])
		b.buf = newBuf
	} else {
		b.buf = nil
	}

	return toEncode
}

// TakeAll returns all data in the buffer and clears the buffer.
func (b *BlockBuffer) TakeAll() []byte {
	ret := b.buf
	b.buf = nil
	return ret
}
