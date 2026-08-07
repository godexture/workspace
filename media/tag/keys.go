package tag

import "github.com/godexture/godec/media/key"

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
	Title       = key.Define[titleID, string]()
	Artist      = key.Define[artistID, string]()
	Album       = key.Define[albumID, string]()
	Date        = key.Define[dateID, PartialDate]()
	Genre       = key.Define[genreID, string]()
	Comment     = key.Define[commentID, string]()
	Composer    = key.Define[composerID, string]()
	Lyrics      = key.Define[lyricsID, string]()
	Website     = key.Define[websiteID, string]()
	TrackNumber = key.Define[trackNumberID, int64]()
	TotalTracks = key.Define[totalTracksID, int64]()
	DiscNumber  = key.Define[discNumberID, int64]()
	TotalDiscs  = key.Define[totalDiscsID, int64]()
	Copyright   = key.Define[copyrightID, string]()
	License     = key.Define[licenseID, string]()
	Encoder     = key.Define[encoderID, string]()
	// Artwork holds an immutable Blob, so copying the value is already a
	// snapshot. The clone is declared to say so, not to duplicate anything.
	Picture = key.Define[pictureID, Artwork](func(value Artwork) Artwork { return value })
)
