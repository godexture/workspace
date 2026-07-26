// Command server runs the HTTP API for the Web conversion demo: catalog and
// preset lookup, pipeline preview, and Server-mode conversion jobs. Pair it
// with the Vite dev server in example/web/client during development, or
// point -static at its production build to serve everything from one
// process.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"github.com/godexture/web/internal/api"
	"github.com/godexture/web/internal/jobs"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-mp3"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/filter-audio"
	_ "github.com/godexture/format-flac"
	_ "github.com/godexture/format-mp3"
	_ "github.com/godexture/format-wav"
)

const maxUploadBytes = 1 << 30 // Server mode limit: 1 GiB

func main() {
	addr := flag.String("addr", ":8787", "HTTP listen address")
	assetsDir := flag.String("assets", defaultPath("../../assets"), "Directory containing preset audio files")
	tempDir := flag.String("temp-dir", "", "Directory for job input/output files (default: a fresh OS temp dir, removed on exit)")
	flag.Parse()

	dir := *tempDir
	if dir == "" {
		created, err := os.MkdirTemp("", "godec-web-*")
		if err != nil {
			log.Fatalf("create temp dir: %v", err)
		}
		dir = created
		defer os.RemoveAll(dir)
	}

	store, err := jobs.NewStore(dir)
	if err != nil {
		log.Fatalf("create job store: %v", err)
	}
	e := api.New(store, *assetsDir, maxUploadBytes)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = e.Shutdown(context.Background())
	}()

	fmt.Printf("Listening on %s (assets=%s jobs=%s)\n", *addr, *assetsDir, dir)
	if err := e.Start(*addr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}

// defaultPath resolves relative to this source file so the binary finds
// example/assets and example/web/client/dist regardless of the working
// directory it's run from.
func defaultPath(relative string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), relative)
}
