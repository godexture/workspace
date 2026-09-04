package frame

import "errors"

var (
	ErrIndexTooShort = errors.New("mp3 index header is too short")
	ErrIndexInvalid  = errors.New("invalid mp3 index header")
)

// XingKind identifies the marker used by a Xing index.
type XingKind uint8

const (
	XingKindXing XingKind = iota + 1
	XingKindInfo
)

// Xing is an immutable value containing the present fields of a Xing/Info
// index. TOC returns a copy of the fixed-size table.
type Xing struct {
	kind    XingKind
	flags   uint32
	frames  uint32
	bytes   uint32
	toc     [100]byte
	quality uint32
}

func (x Xing) Kind() XingKind { return x.kind }
func (x Xing) Flags() uint32  { return x.flags }

func (x Xing) HasFrames() bool { return x.flags&0x0001 != 0 }
func (x Xing) Frames() uint32  { return x.frames }

func (x Xing) HasBytes() bool { return x.flags&0x0002 != 0 }
func (x Xing) Bytes() uint32  { return x.bytes }

func (x Xing) HasTOC() bool   { return x.flags&0x0004 != 0 }
func (x Xing) TOC() [100]byte { return x.toc }

func (x Xing) HasQuality() bool { return x.flags&0x0008 != 0 }
func (x Xing) Quality() uint32  { return x.quality }

// VBRI is an immutable value containing a bounded Fraunhofer VBR index. TOC
// contains raw table entries and returns a copy of the decoded table.
type VBRI struct {
	version        uint16
	delay          uint16
	quality        uint16
	bytes          uint32
	frames         uint32
	entries        uint16
	scale          uint16
	entrySize      uint16
	framesPerEntry uint16
	toc            []uint32
}

func (v VBRI) Version() uint16        { return v.version }
func (v VBRI) Delay() uint16          { return v.delay }
func (v VBRI) Quality() uint16        { return v.quality }
func (v VBRI) Bytes() uint32          { return v.bytes }
func (v VBRI) Frames() uint32         { return v.frames }
func (v VBRI) EntryCount() uint16     { return v.entries }
func (v VBRI) Scale() uint16          { return v.scale }
func (v VBRI) EntrySize() uint16      { return v.entrySize }
func (v VBRI) FramesPerEntry() uint16 { return v.framesPerEntry }
func (v VBRI) TOC() []uint32          { return append([]uint32(nil), v.toc...) }
