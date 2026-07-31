package metadata

import (
	"github.com/godexture/godec/sdk/date"
)

type KeyId string
type KeyFilename string
type KeySize uint64
type KeyMimeType string
type KeyDate date.Partial
type KeyTitle string
type KeyDescription string
type KeyArtist string
type KeyComposer string
type KeyComment string
type KeyLyrics string
type KeyWebsite string
type KeyAlbum string
type KeyGenre string
type KeyTrackNumber int64
type KeyTotalTracks int64
type KeyDiscNumber int64
type KeyTotalDiscs int64
type KeyTag string
type KeyThumbnail []Thumbnail
type KeyCopyright string
type KeyLicense string
type KeyEncoder string

func (k KeyId) single()          {}
func (k KeyFilename) single()    {}
func (k KeySize) single()        {}
func (k KeyMimeType) single()    {}
func (k KeyDate) single()        {}
func (k KeyTitle) single()       {}
func (k KeyDescription) single() {}

func (k KeyComposer) single()    {}
func (k KeyComment) single()     {}
func (k KeyLyrics) single()      {}
func (k KeyWebsite) single()     {}
func (k KeyAlbum) single()       {}
func (k KeyGenre) single()       {}
func (k KeyTrackNumber) single() {}
func (k KeyTotalTracks) single() {}
func (k KeyDiscNumber) single()  {}
func (k KeyTotalDiscs) single()  {}
func (k KeyTag) single()         {}
func (k KeyThumbnail) single()   {}
func (k KeyCopyright) single()   {}
func (k KeyLicense) single()     {}
func (k KeyEncoder) single()     {}
func (k keyRaw) single()         {}

func (k KeyTag) multiple()       {}
func (k KeyThumbnail) multiple() {}
func (v KeyArtist) multiple()    {}
func (v KeyWebsite) multiple()   {}
