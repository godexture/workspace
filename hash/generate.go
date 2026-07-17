//go:build ignore

package main

import (
	"log"

	generator "github.com/godexture/tools/table-generator"
)

func buildCRC8Table() [256]byte {
	var table [256]byte
	for i := range table {
		crc := byte(i)
		for j := 0; j < 8; j++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}

func buildCRC16Table() [256]uint16 {
	var table [256]uint16
	for i := range table {
		crc := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x8005
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}

func crc16ZeroStep(crc uint16, crc16Table [256]uint16) uint16 {
	return crc<<8 ^ crc16Table[crc>>8]
}

type crc16Slice8Tables struct {
	byteTable [8][256]uint16
	stateHi   [256]uint16
	stateLo   [256]uint16
}

func buildCRC16Slice8(crc16Table [256]uint16) crc16Slice8Tables {
	var tables crc16Slice8Tables

	// baseTable[m][b] = CRC16Update(0, [b] followed by m zero bytes).
	var baseTable [8][256]uint16
	baseTable[0] = crc16Table
	for m := 1; m < 8; m++ {
		for b := range 256 {
			baseTable[m][b] = crc16ZeroStep(baseTable[m-1][b], crc16Table)
		}
	}
	// Byte at block position j (0-indexed) is followed by 7-j zero bytes.
	for j := 0; j < 8; j++ {
		tables.byteTable[j] = baseTable[7-j]
	}

	for h := range 256 {
		crc := uint16(h) << 8
		for i := 0; i < 8; i++ {
			crc = crc16ZeroStep(crc, crc16Table)
		}
		tables.stateHi[h] = crc
	}
	for l := range 256 {
		crc := uint16(l)
		for i := 0; i < 8; i++ {
			crc = crc16ZeroStep(crc, crc16Table)
		}
		tables.stateLo[l] = crc
	}
	return tables
}

func main() {
	crc8Table := buildCRC8Table()
	crc16Table := buildCRC16Table()
	slice8 := buildCRC16Slice8(crc16Table)

	tables := []generator.Table{
		{
			Name:   "crc8Table",
			Type:   "[256]byte",
			Data:   crc8Table,
			Format: "%#02x",
		},
		{
			Name:   "crc16Table",
			Type:   "[256]uint16",
			Data:   crc16Table,
			Format: "%#04x",
		},
		{
			Name:   "crc16Slice8",
			Type:   "crc16Slice8Tables",
			Data:   slice8,
			Format: "%#04x",
		},
	}

	if err := generator.Generate("tables.go", "hash", tables); err != nil {
		log.Fatalf("generate error: %v", err)
	}
}
