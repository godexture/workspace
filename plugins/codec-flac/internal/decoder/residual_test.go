package decoder

import (
	"testing"

	"github.com/godexture/sdk/bits"
)

func TestDecodeResidualRiceRange(t *testing.T) {
	tests := []struct {
		name     string
		unsigned uint64
		want     int64
		wantErr  bool
	}{
		{name: "maximum", unsigned: 0xfffffffe, want: 2147483647},
		{name: "minimum", unsigned: 0xfffffffd, want: -2147483647},
		{name: "excluded minimum", unsigned: 0xffffffff, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var writer bits.Writer
			writer.Bits64(1, 2)
			writer.Bits64(0, 4)
			writer.Bits64(30, 5)
			writer.UnaryBits64(test.unsigned, 30)

			reader := bits.New(writer.Bytes())
			residual := make([]int64, 1)
			err := DecodeResidualInto(reader, residual, 1, 0)
			if test.wantErr {
				if err == nil {
					t.Fatal("DecodeResidualInto() error = nil, want range error")
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodeResidualInto() error = %v", err)
			}
			if residual[0] != test.want {
				t.Fatalf("residual = %d, want %d", residual[0], test.want)
			}
		})
	}
}
