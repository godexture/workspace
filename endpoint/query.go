package endpoint

import (
	"errors"

	"github.com/godexture/godec/plugin"
)

var ErrInvalidDeviceQuery = errors.New("endpoint device query is invalid")

// DeviceQuery is an explicit opt-in request for enumeration. Constructing a
// query has no scan, permission, or network side effect.
type DeviceQuery struct {
	component plugin.Identity
}

func NewDeviceQuery(component plugin.Identity) (DeviceQuery, error) {
	if component.IsZero() {
		return DeviceQuery{}, ErrInvalidDeviceQuery
	}
	return DeviceQuery{component: component}, nil
}

func (q DeviceQuery) Valid() bool                        { return !q.component.IsZero() }
func (q DeviceQuery) ComponentIdentity() plugin.Identity { return q.component }
