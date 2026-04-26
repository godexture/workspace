package media

import "fmt"

type MediaType string

const (
	MediaUnknown    MediaType = ""
	MediaVideo      MediaType = "video"
	MediaAudio      MediaType = "audio"
	MediaSubtitle   MediaType = "subtitle"
	MediaData       MediaType = "data"
	MediaAttachment MediaType = "attachment"
)

type Profile struct {
	Type MediaType

	MediaAttributes
}

func (p Profile) Signature() string {
	return fmt.Sprintf("%v", p)
}
