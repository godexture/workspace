package hash

import (
	"math/rand"
	"testing"
)

// referenceCRC8 and referenceCRC16 are the original bit-serial
// implementations, kept here only to verify the table-driven versions in
// crc.go compute identical results.
func referenceCRC8(data []byte) byte {
	var crc byte
	for _, value := range data {
		crc ^= value
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func referenceCRC16(data []byte) uint16 {
	var crc uint16
	for _, value := range data {
		crc ^= uint16(value) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x8005
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func TestCRC8MatchesBitSerialReference(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 2000; trial++ {
		data := make([]byte, rng.Intn(64))
		rng.Read(data)
		if got, want := CRC8(data), referenceCRC8(data); got != want {
			t.Fatalf("CRC8(% x) = %#02x, want %#02x", data, got, want)
		}
	}
}

func TestCRC16MatchesBitSerialReference(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 2000; trial++ {
		data := make([]byte, rng.Intn(64))
		rng.Read(data)
		if got, want := CRC16(data), referenceCRC16(data); got != want {
			t.Fatalf("CRC16(% x) = %#04x, want %#04x", data, got, want)
		}
	}
}

func TestCRC8SingleBytesExhaustive(t *testing.T) {
	t.Parallel()
	for i := 0; i <= 255; i++ {
		data := []byte{byte(i)}
		if got, want := CRC8(data), referenceCRC8(data); got != want {
			t.Fatalf("CRC8([%d]) = %#02x, want %#02x", i, got, want)
		}
	}
}

func TestCRC16SingleBytesExhaustive(t *testing.T) {
	t.Parallel()
	for i := 0; i <= 255; i++ {
		data := []byte{byte(i)}
		if got, want := CRC16(data), referenceCRC16(data); got != want {
			t.Fatalf("CRC16([%d]) = %#04x, want %#04x", i, got, want)
		}
	}
}

func BenchmarkCRC16(b *testing.B) {
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i * 37)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CRC16(data)
	}
}
