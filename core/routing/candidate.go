package routing

import "github.com/godexture/core/domain/media"

type Candidate interface {
	ID() string

	Accept(p media.Profile) bool
	Transform(p media.Profile) media.Profile
}
