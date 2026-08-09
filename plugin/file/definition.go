// Package file implements local-file Access source and sink components.
package file

import "github.com/godexture/godec/plugin"

type (
	pluginID struct{}
	sourceID struct{}
	sinkID   struct{}
)

func SourceIdentity() plugin.Identity { return plugin.IdentityOf[sourceID]() }
func SinkIdentity() plugin.Identity   { return plugin.IdentityOf[sinkID]() }

// Plugin returns the local-file component family without global registration.
func Plugin() plugin.Definition {
	descriptor := plugin.Descriptor{
		DisplayName: "File Access",
		Version:     "0.1.0",
		License:     "MIT",
		Build:       plugin.BuildModePureGo,
	}
	return plugin.Define[pluginID](descriptor, sourceComponent(), sinkComponent())
}
