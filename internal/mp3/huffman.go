package mp3

import (
	"unsafe"
)

// bs_t is the bitstream reader state.
type bs_t struct {
	buf   *byte
	pos   int32
	limit int32
}

// L3GrInfo matches the C structure L3_gr_info_t layout exactly.
type L3GrInfo struct {
	sfbtab           *byte
	part23Length     uint16
	bigValues        uint16
	scalefacCompress uint16
	globalGain       uint8
	blockType        uint8
	mixedBlockFlag   uint8
	nLongSfb         uint8
	nShortSfb        uint8
	tableSelect      [3]uint8
	regionCount      [3]uint8
	subblockGain     [3]uint8
	preflag          uint8
	scalefacScale    uint8
	count1Table      uint8
	scfsi            uint8
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func L3Pow43(x int) float32 {
	var frac float32
	var sign, mult int = 0, 256

	if x < 129 {
		return gPow43[16+x]
	}

	if x < 1024 {
		mult = 16
		x <<= 3
	}

	sign = (2 * x) & 64
	frac = float32((x&63)-sign) / float32((x&^63)+sign)
	return gPow43[16+((x+sign)>>6)] * (1.0 + frac*(4.0/3.0+frac*(2.0/9.0))) * float32(mult)
}

func getSfbVal(sfbtab *byte, idx int) byte {
	return *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(sfbtab)) + uintptr(idx)))
}

// L3HuffmanDecode performs Huffman decoding for a Layer 3 granule.
func L3HuffmanDecode(dst []float32, bsPtr unsafe.Pointer, grInfoPtr unsafe.Pointer, scf []float32, regionLimit int) {
	if len(dst) == 0 || bsPtr == nil || grInfoPtr == nil {
		return
	}

	bs := (*bs_t)(bsPtr)
	grInfo := (*L3GrInfo)(grInfoPtr)

	bsBuf := uintptr(unsafe.Pointer(bs.buf))
	bsNextPtr := bsBuf + uintptr(bs.pos/8)

	readByte := func(p uintptr) uint32 {
		return uint32(*(*byte)(unsafe.Pointer(p)))
	}

	bsCache := (((readByte(bsNextPtr)*256+readByte(bsNextPtr+1))*256+readByte(bsNextPtr+2))*256 + readByte(bsNextPtr+3)) << (uint32(bs.pos) & 7)
	bsSh := int32((bs.pos & 7) - 8)
	bsNextPtr += 4

	peekBits := func(n int) uint32 {
		return bsCache >> (32 - n)
	}
	flushBits := func(n int) {
		bsCache <<= n
		bsSh += int32(n)
	}
	checkBits := func() {
		for bsSh >= 0 {
			val := readByte(bsNextPtr)
			bsNextPtr++
			bsCache |= val << bsSh
			bsSh -= 8
		}
	}
	bsPos := func() int {
		return int((bsNextPtr-bsBuf)*8 - 24 + uintptr(bsSh))
	}

	one := float32(0.0)
	ireg := 0
	bigValCnt := int(grInfo.bigValues)
	sfb := grInfo.sfbtab
	sfbIdx := 0
	dstIdx := 0
	scfIdx := 0

	for bigValCnt > 0 {
		tabNum := int(grInfo.tableSelect[ireg])
		sfbCnt := int(grInfo.regionCount[ireg])
		ireg++
		codebook := tabs[tabindex[tabNum]:]
		linbits := int(gLinbits[tabNum])

		if linbits > 0 {
			for {
				np := int(getSfbVal(sfb, sfbIdx)) / 2
				sfbIdx++
				pairsToDecode := min(bigValCnt, np)
				one = scf[scfIdx]
				scfIdx++
				for pairsToDecode > 0 {
					w := 5
					leaf := int(codebook[peekBits(w)])
					for leaf < 0 {
						flushBits(w)
						w = leaf & 7
						leaf = int(codebook[int(peekBits(w))-(leaf>>3)])
					}
					flushBits(leaf >> 8)

					for j := 0; j < 2; j++ {
						lsb := leaf & 0x0F
						if lsb == 15 {
							lsb += int(peekBits(linbits))
							flushBits(linbits)
							checkBits()
							val := one * L3Pow43(lsb)
							if int32(bsCache) < 0 {
								val = -val
							}
							dst[dstIdx] = val
						} else {
							index := 16 + lsb - 16*int(bsCache>>31)
							dst[dstIdx] = gPow43[index] * one
						}
						dstIdx++
						if lsb != 0 {
							flushBits(1)
						}
						leaf >>= 4
					}
					checkBits()
					pairsToDecode--
				}
				bigValCnt -= np
				sfbCnt--
				if bigValCnt <= 0 || sfbCnt < 0 {
					break
				}
			}
		} else {
			for {
				np := int(getSfbVal(sfb, sfbIdx)) / 2
				sfbIdx++
				pairsToDecode := min(bigValCnt, np)
				one = scf[scfIdx]
				scfIdx++
				for pairsToDecode > 0 {
					w := 5
					leaf := int(codebook[peekBits(w)])
					for leaf < 0 {
						flushBits(w)
						w = leaf & 7
						leaf = int(codebook[int(peekBits(w))-(leaf>>3)])
					}
					flushBits(leaf >> 8)

					for j := 0; j < 2; j++ {
						lsb := leaf & 0x0F
						index := 16 + lsb - 16*int(bsCache>>31)
						dst[dstIdx] = gPow43[index] * one
						dstIdx++
						if lsb != 0 {
							flushBits(1)
						}
						leaf >>= 4
					}
					checkBits()
					pairsToDecode--
				}
				bigValCnt -= np
				sfbCnt--
				if bigValCnt <= 0 || sfbCnt < 0 {
					break
				}
			}
		}
	}

	np := 1 - bigValCnt
	for {
		var codebookCount1 []byte
		if grInfo.count1Table != 0 {
			codebookCount1 = tab33[:]
		} else {
			codebookCount1 = tab32[:]
		}

		leaf := int(codebookCount1[peekBits(4)])
		if (leaf & 8) == 0 {
			leaf = int(codebookCount1[(leaf>>3)+int((bsCache<<4)>>(32-(leaf&3)))])
		}
		flushBits(leaf & 7)
		if bsPos() > regionLimit {
			break
		}

		reloadScalefactor := func() bool {
			np--
			if np == 0 {
				val := int(getSfbVal(sfb, sfbIdx))
				sfbIdx++
				np = val / 2
				if np == 0 {
					return false
				}
				one = scf[scfIdx]
				scfIdx++
			}
			return true
		}

		deqCount1 := func(s int) {
			if (leaf & (128 >> s)) != 0 {
				val := one
				if int32(bsCache) < 0 {
					val = -one
				}
				dst[dstIdx+s] = val
				flushBits(1)
			}
		}

		if !reloadScalefactor() {
			break
		}
		deqCount1(0)
		deqCount1(1)
		if !reloadScalefactor() {
			break
		}
		deqCount1(2)
		deqCount1(3)

		dstIdx += 4
		checkBits()
	}

	bs.pos = int32(regionLimit)
}
