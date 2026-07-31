package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/godexture/godec/example/web/server/internal/jobs"
	"github.com/godexture/godec/sdk/conversion"
	"github.com/labstack/echo/v4"
)

const progressInterval = 250 * time.Millisecond

// handleEvents streams a job's progress as plain SSE "message" events at a
// fixed interval and closes the stream once the job leaves the running
// state (the final event's Status/Error already say why, so there is no
// separate "done"/"error" event type -- Server and WASM polling both end on
// the same condition: status != "running"). It does not cancel the job on
// disconnect: the job keeps running server-side, and a client can reconnect
// to resume watching it (only DELETE cancels a job).
func (s *Server) handleEvents(c echo.Context) error {
	job, ok := s.jobs.Get(c.Param("id"))
	if !ok {
		return writeError(c, notFoundJob(c.Param("id")))
	}

	response := c.Response()
	response.Header().Set(echo.HeaderContentType, "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("Connection", "keep-alive")
	response.WriteHeader(http.StatusOK)

	if writeProgressEvent(response, job) {
		return nil
	}

	ticker := time.NewTicker(progressInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-job.Done():
			writeProgressEvent(response, job)
			return nil
		case <-ticker.C:
			if writeProgressEvent(response, job) {
				return nil
			}
		}
	}
}

// writeProgressEvent sends one progress tick and reports whether the job
// has finished (so the caller can stop streaming).
func writeProgressEvent(response *echo.Response, job *jobs.Job) bool {
	snapshot := job.Snapshot()
	data, err := json.Marshal(snapshot)
	if err != nil {
		return true
	}
	_, _ = fmt.Fprintf(response, "data: %s\n\n", data)
	response.Flush()
	return snapshot.Status != conversion.StatusRunning
}
