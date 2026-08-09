package access

import (
	"errors"
	"fmt"
)

var ErrCapabilityView = errors.New("access session does not provide its selected capability view")

type viewSet struct {
	sequential Sequential
	random     Random
	appender   Appender
	patcher    Patcher
}

// viewsFor is the single capability-to-view mapping used when Prepare narrows
// an acquired session. Semantic capabilities have no operation view.
func viewsFor(session Session, selection Selection) (viewSet, error) {
	if session == nil || !selection.Valid() {
		return viewSet{}, ErrCapabilityView
	}
	var result viewSet
	for _, capability := range selection.capabilities {
		switch capability {
		case SequentialRead:
			result.sequential, _ = session.(Sequential)
			if result.sequential == nil {
				return viewSet{}, missingCapabilityView(capability)
			}
		case RandomRead:
			result.random, _ = session.(Random)
			if result.random == nil {
				return viewSet{}, missingCapabilityView(capability)
			}
		case SequentialWrite:
			result.appender, _ = session.(Appender)
			if result.appender == nil {
				return viewSet{}, missingCapabilityView(capability)
			}
		case RandomWrite:
			result.patcher, _ = session.(Patcher)
			if result.patcher == nil {
				return viewSet{}, missingCapabilityView(capability)
			}
		}
	}
	return result, nil
}

func missingCapabilityView(capability Capability) error {
	return fmt.Errorf("%w: %s", ErrCapabilityView, capability)
}
