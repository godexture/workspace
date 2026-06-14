package mp3

// L3Dequantize dequantizes decoded Huffman indices to spectral values.
// Note: In minimp3, dequantization is performed inline inside Huffman decoding (L3HuffmanDecode).
func L3Dequantize(xr []float32, grInfo interface{}, scf []float32) {
	// No-op for C wrapper phase, as C.L3_huffman (called by L3HuffmanDecode) handles both.
}

