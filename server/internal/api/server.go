// Package api implements the demo's HTTP surface: catalog/preset
// lookup, pipeline preview, and Server-mode conversion jobs (create, poll
// via SSE, fetch result, cancel).
package api

import (
	"io"
	"net/http"
	"os"

	"github.com/godexture/example-web/internal/jobs"
	"github.com/godexture/sdk/catalog"
	"github.com/godexture/sdk/conversion"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type Server struct {
	jobs           *jobs.Store
	assetsDir      string
	maxUploadBytes int64
	catalog        catalog.Catalog
}

// New builds the HTTP API. assetsDir is where the demo's preset audio
// files live (example/assets); maxUploadBytes bounds the size of any
// uploaded file (Server mode's 1 GiB limit).
func New(jobStore *jobs.Store, assetsDir string, maxUploadBytes int64) *echo.Echo {
	s := &Server{
		jobs:           jobStore,
		assetsDir:      assetsDir,
		maxUploadBytes: maxUploadBytes,
		catalog:        catalog.Build(),
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	api := e.Group("/api")
	api.GET("/catalog", s.handleCatalog)
	api.GET("/presets", s.handlePresets)
	api.GET("/presets/:id/audio", s.handlePresetAudio)
	api.POST("/pipelines/resolve", s.handleResolve)
	api.POST("/conversions", s.handleCreateConversion)
	api.GET("/conversions/:id/events", s.handleEvents)
	api.GET("/conversions/:id/result", s.handleResult)
	api.DELETE("/conversions/:id", s.handleDelete)

	return e
}

func (s *Server) handleCatalog(c echo.Context) error {
	return writeJSON(c, http.StatusOK, s.catalog)
}

func (s *Server) handlePresets(c echo.Context) error {
	return writeJSON(c, http.StatusOK, presetTable)
}

func (s *Server) handlePresetAudio(c echo.Context) error {
	preset, ok := findPreset(c.Param("id"))
	if !ok {
		return writeError(c, notFoundPreset(c.Param("id")))
	}
	c.Response().Header().Set(echo.HeaderContentType, preset.ContentType)
	return c.File(s.presetPath(preset))
}

func (s *Server) handleResolve(c echo.Context) error {
	if err := s.parseUpload(c); err != nil {
		return writeError(c, err)
	}
	spec, err := parseSpec(c)
	if err != nil {
		return writeError(c, err)
	}
	input, err := s.openPreviewInput(c)
	if err != nil {
		return writeError(c, err)
	}
	defer input.Close()

	geometry, err := conversion.Negotiate(c.Request().Context(), conversion.InputSet{Main: input}, io.Discard, spec)
	if err != nil {
		return writeError(c, err)
	}
	defer geometry.Close()

	return writeJSON(c, http.StatusOK, geometry.Description())
}

func (s *Server) handleCreateConversion(c echo.Context) error {
	if err := s.parseUpload(c); err != nil {
		return writeError(c, err)
	}
	spec, err := parseSpec(c)
	if err != nil {
		return writeError(c, err)
	}
	path, owned, err := s.prepareJobInput(c)
	if err != nil {
		return writeError(c, err)
	}

	job, err := s.jobs.Start(path, owned, spec)
	if err != nil {
		if owned {
			_ = os.Remove(path)
		}
		return writeError(c, err)
	}
	return writeJSON(c, http.StatusCreated, map[string]string{"id": job.ID})
}

func (s *Server) handleResult(c echo.Context) error {
	id := c.Param("id")
	job, ok := s.jobs.Get(id)
	if !ok {
		return writeError(c, notFoundJob(id))
	}
	select {
	case <-job.Done():
	default:
		return writeError(c, conversion.NewError(conversion.CodeNotReady, "job is still running"))
	}
	if err := job.Err(); err != nil {
		return writeError(c, err)
	}
	return c.File(job.OutputPath())
}

func (s *Server) handleDelete(c echo.Context) error {
	id := c.Param("id")
	if _, ok := s.jobs.Get(id); !ok {
		return writeError(c, notFoundJob(id))
	}
	s.jobs.Remove(id)
	return c.NoContent(http.StatusNoContent)
}

func notFoundJob(id string) error {
	return conversion.NewError(conversion.CodeNotFound, "unknown job "+id)
}
