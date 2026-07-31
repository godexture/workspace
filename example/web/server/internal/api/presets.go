package api

import (
	"path/filepath"

	"github.com/godexture/godec/sdk/conversion"
)

type Preset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
}

// presetTable describes the fixed demo assets under example/assets. It is a
// small hand-maintained list (not a directory listing) so each preset gets
// a readable Japanese label and a correct content type.
var presetTable = []Preset{
	{ID: "lpcm", Name: "PCM", Filename: "lpcm.wav", ContentType: "audio/wav"},
	{ID: "adpcm-ima", Name: "IMA ADPCM", Filename: "adpcm_ima.wav", ContentType: "audio/wav"},
	{ID: "adpcm-ms", Name: "MS ADPCM", Filename: "adpcm_ms.wav", ContentType: "audio/wav"},
	{ID: "mp3-cbr", Name: "MP3 (CBR)", Filename: "mpeg.mp3", ContentType: "audio/mpeg"},
	{ID: "mp3-vbr", Name: "MP3 (VBR)", Filename: "vbr.mp3", ContentType: "audio/mpeg"},
}

func findPreset(id string) (Preset, bool) {
	for _, preset := range presetTable {
		if preset.ID == id {
			return preset, true
		}
	}
	return Preset{}, false
}

func (s *Server) presetPath(preset Preset) string {
	return filepath.Join(s.assetsDir, preset.Filename)
}

func notFoundPreset(id string) error {
	return conversion.NewError(conversion.CodeNotFound, "unknown preset "+id)
}
