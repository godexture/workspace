package tag

import (
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/plugin"
)

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
	title       = key.Define[titleID, string]()
	artist      = key.Define[artistID, string]()
	album       = key.Define[albumID, string]()
	date        = key.Define[dateID, PartialDate]()
	genre       = key.Define[genreID, string]()
	comment     = key.Define[commentID, string]()
	composer    = key.Define[composerID, string]()
	lyrics      = key.Define[lyricsID, string]()
	website     = key.Define[websiteID, string]()
	trackNumber = key.Define[trackNumberID, int64]()
	totalTracks = key.Define[totalTracksID, int64]()
	discNumber  = key.Define[discNumberID, int64]()
	totalDiscs  = key.Define[totalDiscsID, int64]()
	copyright   = key.Define[copyrightID, string]()
	license     = key.Define[licenseID, string]()
	encoder     = key.Define[encoderID, string]()
	// Artwork holds an immutable Blob, so copying the value is already a
	// snapshot. The clone is declared to say so, not to duplicate anything.
	picture = key.Define[pictureID, Artwork](func(value Artwork) Artwork { return value })
)

func Title() key.Key[string]      { return title }
func Artist() key.Key[string]     { return artist }
func Album() key.Key[string]      { return album }
func Date() key.Key[PartialDate]  { return date }
func Genre() key.Key[string]      { return genre }
func Comment() key.Key[string]    { return comment }
func Composer() key.Key[string]   { return composer }
func Lyrics() key.Key[string]     { return lyrics }
func Website() key.Key[string]    { return website }
func TrackNumber() key.Key[int64] { return trackNumber }
func TotalTracks() key.Key[int64] { return totalTracks }
func DiscNumber() key.Key[int64]  { return discNumber }
func TotalDiscs() key.Key[int64]  { return totalDiscs }
func Copyright() key.Key[string]  { return copyright }
func License() key.Key[string]    { return license }
func Encoder() key.Key[string]    { return encoder }
func Picture() key.Key[Artwork]   { return picture }

// Declarations exposes the shared vocabulary to host composition validation.
// Callers may freely modify the returned slice.
func Declarations() []plugin.Declaration {
	return []plugin.Declaration{
		plugin.DeclareKey(title),
		plugin.DeclareKey(artist),
		plugin.DeclareKey(album),
		plugin.DeclareKey(date),
		plugin.DeclareKey(genre),
		plugin.DeclareKey(comment),
		plugin.DeclareKey(composer),
		plugin.DeclareKey(lyrics),
		plugin.DeclareKey(website),
		plugin.DeclareKey(trackNumber),
		plugin.DeclareKey(totalTracks),
		plugin.DeclareKey(discNumber),
		plugin.DeclareKey(totalDiscs),
		plugin.DeclareKey(copyright),
		plugin.DeclareKey(license),
		plugin.DeclareKey(encoder),
		plugin.DeclareKey(picture),
	}
}
