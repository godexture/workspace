package tag

import "github.com/godexture/godec/media/metadata"

type (
	titleID       struct{}
	artistID      struct{}
	albumID       struct{}
	dateID        struct{}
	genreID       struct{}
	commentID     struct{}
	composerID    struct{}
	lyricsID      struct{}
	websiteID     struct{}
	trackNumberID struct{}
	totalTracksID struct{}
	discNumberID  struct{}
	totalDiscsID  struct{}
	copyrightID   struct{}
	licenseID     struct{}
	encoderID     struct{}
	pictureID     struct{}
)

// The vocabulary is the union of fields currently read or written by ID3,
// Vorbis Comment, and RIFF INFO. It deliberately does not include unused
// legacy Bundle keys.
var (
	Title       = metadata.DefineKey[titleID, string]()
	Artist      = metadata.DefineKey[artistID, string]()
	Album       = metadata.DefineKey[albumID, string]()
	Date        = metadata.DefineKey[dateID, PartialDate]()
	Genre       = metadata.DefineKey[genreID, string]()
	Comment     = metadata.DefineKey[commentID, string]()
	Composer    = metadata.DefineKey[composerID, string]()
	Lyrics      = metadata.DefineKey[lyricsID, string]()
	Website     = metadata.DefineKey[websiteID, string]()
	TrackNumber = metadata.DefineKey[trackNumberID, int64]()
	TotalTracks = metadata.DefineKey[totalTracksID, int64]()
	DiscNumber  = metadata.DefineKey[discNumberID, int64]()
	TotalDiscs  = metadata.DefineKey[totalDiscsID, int64]()
	Copyright   = metadata.DefineKey[copyrightID, string]()
	License     = metadata.DefineKey[licenseID, string]()
	Encoder     = metadata.DefineKey[encoderID, string]()
	// Artwork holds an immutable Blob, so copying the value is already a
	// snapshot. The clone is declared to say so, not to duplicate anything.
	Picture = metadata.DefineKey[pictureID, Artwork](func(value Artwork) Artwork { return value })
)
