package linear

import (
	"errors"
	"unsafe"

	"github.com/godexture/godec/media/sample"
)

// nativeEndian is the byte order of the Go scalar types a canonical frame
// stores. A wire coding in the other order is normalized by reversing whole
// samples in the scratch block, which keeps every unpack loop native-order.
var nativeEndian = nativeByteOrder()

func nativeByteOrder() sample.Endian {
	value := uint16(1)
	if *(*byte)(unsafe.Pointer(&value)) == 1 {
		return sample.LittleEndian
	}
	return sample.BigEndian
}

var errSampleWidth = errors.New("linear PCM sample width has no byte-order normalization")

// swapWidth reports the sample width whose bytes have to be reversed to reach
// native order, or zero when the wire is already native. Three-byte samples
// have no scalar type to reverse into, so their unpack loops read the wire
// order directly and this reports zero for them.
func swapWidth(description sample.Description) int {
	width := description.Coding.Bytes()
	if width == 1 || width == 3 || description.Endian == nativeEndian {
		return 0
	}
	return width
}

// reverseSamples reverses every width-byte sample in block. block is the
// caller's scratch copy of the wire, so reversing it in place never reaches
// the borrowed source payload.
func reverseSamples(block []byte, width int) error {
	switch width {
	case 2:
		for index := 0; index+2 <= len(block); index += 2 {
			block[index], block[index+1] = block[index+1], block[index]
		}
	case 4:
		for index := 0; index+4 <= len(block); index += 4 {
			block[index], block[index+3] = block[index+3], block[index]
			block[index+1], block[index+2] = block[index+2], block[index+1]
		}
	case 8:
		for index := 0; index+8 <= len(block); index += 8 {
			block[index], block[index+7] = block[index+7], block[index]
			block[index+1], block[index+6] = block[index+6], block[index+1]
			block[index+2], block[index+5] = block[index+5], block[index+2]
			block[index+3], block[index+4] = block[index+4], block[index+3]
		}
	default:
		return errSampleWidth
	}
	return nil
}
