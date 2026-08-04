package metadata

import "github.com/godexture/godec/plugin"

// These markers stand in for a shared vocabulary package and for a third-party
// plugin that adds its own keys without changing core.
type (
	titleID   struct{}
	artistID  struct{}
	artworkID struct{}
	moodID    struct{}
	genreID   struct{}
	ratingID  struct{}
	encodingA struct{}
	encodingB struct{}
)

// Picture is the shape a vocabulary package would give artwork: a description
// plus an immutable payload that entry copies share.
type Picture struct {
	Description string
	Data        Blob
}

var (
	title  = DefineKey[titleID, string]()
	artist = DefineKey[artistID, string]()
	genre  = DefineKey[genreID, string]()
	mood   = DefineKey[moodID, string]()
	rating = DefineKey[ratingID, int]()
	// A Picture holds a Blob, whose backing is immutable, so copying the value
	// is already a snapshot. The clone is declared to say so rather than to
	// duplicate anything.
	artwork = DefineKey[artworkID, Picture](func(value Picture) Picture { return value })
)

func encodingIdentity() plugin.Identity      { return plugin.IdentityOf[encodingA]() }
func otherEncodingIdentity() plugin.Identity { return plugin.IdentityOf[encodingB]() }
