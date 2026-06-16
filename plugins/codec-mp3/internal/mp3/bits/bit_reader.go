package bits

// BitReader is the bit stream reader state.
type BitReader struct {
	Buffer   []byte
	Position int32
	Limit    int32
}

// Init initializes the BitReader.
func (r *BitReader) Init(buffer []byte, position int32, limit int32) {
	r.Buffer = buffer
	r.Position = position
	r.Limit = limit
}

// GetBits reads the specified number of bits from the stream.
func (r *BitReader) GetBits(width int) uint32 {
	bitOffset := r.Position & 7
	shiftLeft := width + int(bitOffset)
	byteIndex := int(r.Position >> 3)
	r.Position += int32(width)
	if r.Position > r.Limit {
		return 0
	}
	readByte := func(index int) uint32 {
		if index < 0 || index >= len(r.Buffer) {
			return 0
		}
		return uint32(r.Buffer[index])
	}
	next := readByte(byteIndex) & (255 >> bitOffset)
	byteIndex++
	bitCache := uint32(0)
	for shiftLeft > 8 {
		shiftLeft -= 8
		bitCache |= next << shiftLeft
		next = readByte(byteIndex)
		byteIndex++
	}
	shiftLeft -= 8
	return bitCache | (next >> -shiftLeft)
}
