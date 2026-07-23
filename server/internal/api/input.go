package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/godexture/sdk/conversion"
	"github.com/labstack/echo/v4"
)

func (s *Server) parseUpload(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, s.maxUploadBytes)
	if err := c.Request().ParseMultipartForm(32 << 20); err != nil {
		return conversion.NewError(conversion.CodePayloadTooLarge, "request body exceeds the server upload limit or is malformed: "+err.Error())
	}
	return nil
}

func parseSpec(c echo.Context) (conversion.Spec, error) {
	var spec conversion.Spec
	raw := c.FormValue("spec")
	if raw == "" {
		return spec, conversion.NewError(conversion.CodeInvalidSpec, "spec field is required")
	}
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return spec, conversion.NewError(conversion.CodeInvalidSpec, "invalid spec: "+err.Error())
	}
	return spec, nil
}

// openPreviewInput opens the request's input (an uploaded file or a preset)
// for a one-shot, request-scoped read. Used by /pipelines/resolve, whose
// negotiation never outlives the handler.
func (s *Server) openPreviewInput(c echo.Context) (io.ReadSeekCloser, error) {
	if presetID := c.FormValue("presetId"); presetID != "" {
		preset, ok := findPreset(presetID)
		if !ok {
			return nil, notFoundPreset(presetID)
		}
		return os.Open(s.presetPath(preset))
	}
	header, err := c.FormFile("file")
	if err != nil {
		return nil, conversion.NewError(conversion.CodeInvalidSpec, "provide either a file upload or a presetId")
	}
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	return file, nil
}

// prepareJobInput resolves the request's input into a path a background Job
// can keep reading after the handler returns. Uploaded files are copied
// into a store-owned temp file (owned=true, deleted by Store.Remove);
// presets point directly at the shared asset file (owned=false, never
// deleted).
func (s *Server) prepareJobInput(c echo.Context) (path string, owned bool, err error) {
	if presetID := c.FormValue("presetId"); presetID != "" {
		preset, ok := findPreset(presetID)
		if !ok {
			return "", false, notFoundPreset(presetID)
		}
		return s.presetPath(preset), false, nil
	}
	header, err := c.FormFile("file")
	if err != nil {
		return "", false, conversion.NewError(conversion.CodeInvalidSpec, "provide either a file upload or a presetId")
	}
	file, err := header.Open()
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	dest, err := s.jobs.CreateInputFile()
	if err != nil {
		return "", false, err
	}
	defer dest.Close()
	if _, err := io.Copy(dest, file); err != nil {
		_ = os.Remove(dest.Name())
		return "", false, err
	}
	return dest.Name(), true, nil
}
