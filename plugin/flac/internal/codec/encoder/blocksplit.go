package encoder

import (
	"github.com/godexture/godec/plugin/flac/internal/codec/config"
)

type blockSpan struct {
	offset   int
	length   int
	analysis *frameAnalysis
}

type blockSplitNode struct {
	blockSpan
	cost  uint64
	split bool
	left  *blockSplitNode
	right *blockSplitNode
}

func chooseBlockSplit(block [][]int64, bitsPerSample int, options config.EncoderConfig, windows *windowSet) ([]blockSpan, error) {
	if options.BlockSplitDepth <= 0 {
		return []blockSpan{{length: len(block[0])}}, nil
	}
	root, err := buildBlockSplitNode(block, 0, len(block[0]), 0, bitsPerSample, options, windows)
	if err != nil {
		return nil, err
	}
	spans := make([]blockSpan, 0, 1<<options.BlockSplitDepth)
	selectBlockSpans(root, &spans)
	return spans, nil
}

func buildBlockSplitNode(block [][]int64, offset, length, depth, bitsPerSample int, options config.EncoderConfig, windows *windowSet) (*blockSplitNode, error) {
	samples := blockSlice(block, offset, length)
	node := &blockSplitNode{blockSpan: blockSpan{offset: offset, length: length}}
	if options.BlockSplitMode == config.BlockSplitExact {
		analysis, err := analyzeFrame(samples, bitsPerSample, options, windows)
		if err != nil {
			return nil, err
		}
		node.analysis, node.cost = analysis, analysis.costBits
	} else {
		node.cost = estimateFrameBits(samples)
	}
	if depth == options.BlockSplitDepth {
		return node, nil
	}
	half := length / 2
	left, err := buildBlockSplitNode(block, offset, half, depth+1, bitsPerSample, options, windows)
	if err != nil {
		node.release()
		return nil, err
	}
	right, err := buildBlockSplitNode(block, offset+half, half, depth+1, bitsPerSample, options, windows)
	if err != nil {
		left.release()
		node.release()
		return nil, err
	}
	node.left, node.right = left, right
	if left.cost+right.cost < node.cost {
		node.cost, node.split = left.cost+right.cost, true
	}
	return node, nil
}

func selectBlockSpans(node *blockSplitNode, spans *[]blockSpan) {
	if !node.split {
		node.left.release()
		node.right.release()
		node.left, node.right = nil, nil
		*spans = append(*spans, node.blockSpan)
		return
	}
	node.analysis.release()
	node.analysis = nil
	selectBlockSpans(node.left, spans)
	selectBlockSpans(node.right, spans)
}

func (node *blockSplitNode) release() {
	if node == nil {
		return
	}
	node.analysis.release()
	node.left.release()
	node.right.release()
}

func blockSlice(block [][]int64, offset, length int) [][]int64 {
	result := make([][]int64, len(block))
	for ch := range block {
		result[ch] = block[ch][offset : offset+length]
	}
	return result
}

func estimateFrameBits(samples [][]int64) uint64 {
	cost := uint64(frameOverheadBits)
	for _, channel := range samples {
		cost += estimateChannelBits(channel)
	}
	return cost
}

func releaseBlockSpans(spans []blockSpan) {
	for i := range spans {
		spans[i].analysis.release()
		spans[i].analysis = nil
	}
}
