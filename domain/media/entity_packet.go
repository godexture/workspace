package media

import (
	"github.com/godexture/core/domain/time"
)

type Packet struct {
	ResourceBase
	data *[]byte

	MediaType   MediaType
	StreamIndex int

	PTS      Pts
	DTS      Dts
	Timebase time.Rational
}

func (p *Packet) Data() []byte { return *p.data }
