package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godexture/example-web/internal/api"
	"github.com/godexture/example-web/internal/jobs"
	"github.com/godexture/example-web/internal/testutil"
	"github.com/godexture/sdk/conversion"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/filter-audio"
	_ "github.com/godexture/format-flac"
	_ "github.com/godexture/format-wav"
)

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	store, err := jobs.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	assetsDir := t.TempDir()
	presetPath := filepath.Join(assetsDir, "lpcm.wav")
	presetFile, err := os.Create(presetPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteWAV(presetFile); err != nil {
		t.Fatal(err)
	}
	presetFile.Close()

	api := api.New(store, assetsDir, 1<<20)
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	return server, assetsDir
}

// multipartUpload builds a multipart/form-data body carrying a spec field
// and either an uploaded "file" (when input is non-nil) or a "presetId"
// field (when presetID is non-empty).
func multipartUpload(t *testing.T, spec string, presetID string, input []byte) (string, io.Reader) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField("spec", spec); err != nil {
		t.Fatal(err)
	}
	if presetID != "" {
		if err := writer.WriteField("presetId", presetID); err != nil {
			t.Fatal(err)
		}
	}
	if input != nil {
		part, err := writer.CreateFormFile("file", "input.wav")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(input); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return writer.FormDataContentType(), &buf
}

func testWAVBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := testutil.WriteWAV(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCatalogAndPresets(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/catalog")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog status = %d", resp.StatusCode)
	}
	var catalog struct {
		Muxers []struct{ Name string } `json:"muxers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Muxers) == 0 {
		t.Fatal("catalog has no muxers")
	}

	presetsResp, err := http.Get(server.URL + "/api/presets")
	if err != nil {
		t.Fatal(err)
	}
	defer presetsResp.Body.Close()
	var presets []api.Preset
	if err := json.NewDecoder(presetsResp.Body).Decode(&presets); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, preset := range presets {
		if preset.ID == "lpcm" {
			found = true
		}
	}
	if !found {
		t.Fatalf("presets missing lpcm: %#v", presets)
	}

	audioResp, err := http.Get(server.URL + "/api/presets/lpcm/audio")
	if err != nil {
		t.Fatal(err)
	}
	defer audioResp.Body.Close()
	if audioResp.StatusCode != http.StatusOK {
		t.Fatalf("preset audio status = %d", audioResp.StatusCode)
	}

	missingResp, err := http.Get(server.URL + "/api/presets/nope/audio")
	if err != nil {
		t.Fatal(err)
	}
	defer missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown preset status = %d, want 404", missingResp.StatusCode)
	}
}

func TestResolvePipelinePreview(t *testing.T) {
	server, _ := newTestServer(t)

	contentType, body := multipartUpload(t, `{"muxer":{"name":"wav"}}`, "", testWAVBytes(t))
	resp, err := http.Post(server.URL+"/api/pipelines/resolve", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d", resp.StatusCode)
	}
	var description struct {
		Nodes []struct{ Plugin string }
	}
	if err := json.NewDecoder(resp.Body).Decode(&description); err != nil {
		t.Fatal(err)
	}
	if len(description.Nodes) == 0 {
		t.Fatal("resolve returned no nodes")
	}
}

// TestResolvePipelinePreviewWithFLACEncoderJSONEncodes guards against a
// regression where NodeDescription.Configuration (the live plugin config
// object, e.g. codec-flac's EncoderConfig with its func-typed Apodizations
// field) was marshaled directly into the response, making any FLAC output
// preview fail with a 500 and an empty body.
func TestResolvePipelinePreviewWithFLACEncoderJSONEncodes(t *testing.T) {
	server, _ := newTestServer(t)

	contentType, body := multipartUpload(t, `{"muxer":{"name":"flac"}}`, "", testWAVBytes(t))
	resp, err := http.Post(server.URL+"/api/pipelines/resolve", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status = %d, body = %s", resp.StatusCode, data)
	}
	var description struct {
		Nodes []struct{ Plugin string }
	}
	if err := json.Unmarshal(data, &description); err != nil {
		t.Fatalf("decode response %q: %v", data, err)
	}
	found := false
	for _, node := range description.Nodes {
		if node.Plugin == "flac" {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolve response has no flac node: %#v", description.Nodes)
	}
}

func TestConversionLifecycleWithUpload(t *testing.T) {
	server, _ := newTestServer(t)

	contentType, body := multipartUpload(t, `{"muxer":{"name":"flac"},"codec":"flac"}`, "", testWAVBytes(t))
	createResp, err := http.Post(server.URL+"/api/conversions", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("create did not return an id")
	}

	events := readSSE(t, server.URL+"/api/conversions/"+created.ID+"/events")
	if !strings.Contains(events, `"status":"completed"`) {
		t.Fatalf("events missing a completed status: %s", events)
	}

	resultResp, err := http.Get(server.URL + "/api/conversions/" + created.ID + "/result")
	if err != nil {
		t.Fatal(err)
	}
	defer resultResp.Body.Close()
	if resultResp.StatusCode != http.StatusOK {
		t.Fatalf("result status = %d", resultResp.StatusCode)
	}
	var output bytes.Buffer
	if _, err := output.ReadFrom(resultResp.Body); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("result body is empty")
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/conversions/"+created.ID, nil)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteResp.StatusCode)
	}

	afterDeleteResp, err := http.Get(server.URL + "/api/conversions/" + created.ID + "/result")
	if err != nil {
		t.Fatal(err)
	}
	defer afterDeleteResp.Body.Close()
	if afterDeleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("result after delete status = %d, want 404", afterDeleteResp.StatusCode)
	}
}

func TestConversionWithPreset(t *testing.T) {
	server, assetsDir := newTestServer(t)

	contentType, body := multipartUpload(t, `{"muxer":{"name":"wav"}}`, "lpcm", nil)
	createResp, err := http.Post(server.URL+"/api/conversions", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", createResp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	readSSE(t, server.URL+"/api/conversions/"+created.ID+"/events")

	deleteReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/conversions/"+created.ID, nil)
	if _, err := http.DefaultClient.Do(deleteReq); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(assetsDir, "lpcm.wav")); err != nil {
		t.Fatalf("preset asset was deleted: %v", err)
	}
}

func TestCreateConversionRejectsUnsupportedCodec(t *testing.T) {
	server, _ := newTestServer(t)

	contentType, body := multipartUpload(t, `{"muxer":{"name":"wav"},"codec":"flac"}`, "", testWAVBytes(t))
	resp, err := http.Post(server.URL+"/api/conversions", contentType, body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var payload struct {
		Error conversion.Error `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != conversion.CodeUnsupportedCodec {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, conversion.CodeUnsupportedCodec)
	}
}

func TestUnknownJobReturns404(t *testing.T) {
	server, _ := newTestServer(t)

	for _, url := range []string{
		server.URL + "/api/conversions/nope/result",
		server.URL + "/api/conversions/nope/events",
	} {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", url, resp.StatusCode)
		}
	}

	deleteReq, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/conversions/nope", nil)
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete unknown status = %d, want 404", deleteResp.StatusCode)
	}
}

// readSSE performs a GET against an SSE endpoint and reads until the server
// closes the stream (handleEvents always closes after done/error), with a
// timeout so a stalled stream fails the test instead of hanging it.
func readSSE(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
