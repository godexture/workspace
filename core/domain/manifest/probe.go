package manifest

import "io"

// ProbeScore represents the confidence level of format detection (0-100).
// Higher scores indicate stronger evidence of a format match.
type ProbeScore int

type Probere func(r io.Reader) ProbeScore

const (
	// ProbeMismatch (0):
	// Negative evidence found. The data explicitly contradicts the format.
	ProbeMismatch ProbeScore = 0

	// ProbeExtensionOnly (10):
	// External hints only (e.g., file extension or MIME type).
	// Content is completely unverified. Used as a last-resort fallback.
	ProbeExtensionOnly ProbeScore = 10

	// ProbeGenericContainer (25):
	// Discovered a generic container header (e.g., "RIFF", "ISOBMFF" structure).
	// The exact internal format or codec remains unknown.
	ProbeGenericContainer ProbeScore = 25

	// ProbeSharedMetadata (40):
	// Discovered shared metadata blocks (e.g., ID3v2 tags).
	// High probability of being a media file, but the stream format is unconfirmed.
	ProbeSharedMetadata ProbeScore = 40

	// ProbeSingleSync (60):
	// Discovered a single occurrence of a sync pattern (e.g., MPEG-TS 0x47).
	// Could potentially be a coincidence within binary data.
	ProbeSingleSync ProbeScore = 60

	// ProbeMultipleSync (80):
	// Discovered multiple consecutive sync patterns at the expected intervals.
	// Statistically highly probable to be the correct format.
	ProbeMultipleSync ProbeScore = 80

	// ProbeIncompleteSignature (90):
	// The primary magic number matched perfectly, but the peek buffer was
	// too small to verify mandatory extended headers. Requires a larger buffer.
	ProbeIncompleteSignature ProbeScore = 90

	// ProbeExactSignature (100):
	// Perfect match of the unique format signature at the correct offset
	// (e.g., "ftypmp42", EBML header). Absolute certainty.
	ProbeExactSignature ProbeScore = 100
)
