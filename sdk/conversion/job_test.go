package conversion_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/conversion"

	_ "github.com/godexture/godec/plugins/codec-flac"
	_ "github.com/godexture/godec/plugins/codec-pcm"
	_ "github.com/godexture/godec/plugins/format-flac"
	_ "github.com/godexture/godec/plugins/format-wav"
)

func TestJobRunsToCompletion(t *testing.T) {
	var wav bytes.Buffer
	writeTestWAV(t, &wav)

	var flac bytes.Buffer
	job, err := conversion.StartJob(context.Background(), conversion.InputSet{Main: bytes.NewReader(wav.Bytes())}, &flac, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "flac"},
		Codec: string(media.CodecFLAC),
	})
	if err != nil {
		t.Fatalf("StartJob() error = %v", err)
	}
	defer job.Close()

	select {
	case <-job.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("job did not finish in time")
	}

	if err := job.Err(); err != nil {
		t.Fatalf("job.Err() = %v", err)
	}
	snapshot := job.Snapshot()
	if snapshot.Status != conversion.StatusCompleted {
		t.Fatalf("snapshot.Status = %v, want %v", snapshot.Status, conversion.StatusCompleted)
	}
	if snapshot.Percent != 100 {
		t.Fatalf("snapshot.Percent = %v, want 100", snapshot.Percent)
	}
	if flac.Len() == 0 {
		t.Fatal("job produced no output")
	}
}

func TestJobCancel(t *testing.T) {
	var wav bytes.Buffer
	writeTestWAV(t, &wav)

	job, err := conversion.StartJob(context.Background(), conversion.InputSet{Main: bytes.NewReader(wav.Bytes())}, &bytes.Buffer{}, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
	})
	if err != nil {
		t.Fatalf("StartJob() error = %v", err)
	}
	job.Cancel()

	select {
	case <-job.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("canceled job did not finish in time")
	}
	if got := job.Snapshot().Status; got != conversion.StatusCanceled && got != conversion.StatusCompleted {
		// A cancel racing a very fast conversion may still complete first.
		t.Fatalf("snapshot.Status = %v, want %v or %v", got, conversion.StatusCanceled, conversion.StatusCompleted)
	}
	if err := job.Close(); err != nil {
		t.Fatalf("job.Close() error = %v", err)
	}
}
