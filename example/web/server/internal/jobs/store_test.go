package jobs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/godexture/godec/example/web/server/internal/jobs"
	"github.com/godexture/godec/example/web/server/internal/testutil"
	"github.com/godexture/godec/sdk/conversion"

	_ "github.com/godexture/godec/plugin/pcm"
	_ "github.com/godexture/godec/plugin/audio"
	_ "github.com/godexture/godec/plugin/wave"
)

func newStore(t *testing.T) *jobs.Store {
	t.Helper()
	store, err := jobs.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}

func writeInput(t *testing.T, store *jobs.Store) string {
	t.Helper()
	file, err := store.CreateInputFile()
	if err != nil {
		t.Fatalf("CreateInputFile() error = %v", err)
	}
	defer file.Close()
	if err := testutil.WriteWAV(file); err != nil {
		t.Fatalf("WriteWAV() error = %v", err)
	}
	return file.Name()
}

func waitDone(t *testing.T, job *jobs.Job) {
	t.Helper()
	select {
	case <-job.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("job did not finish in time")
	}
}

func ownedInput(path string) jobs.Inputs {
	return jobs.Inputs{Main: jobs.Input{Path: path, Owned: true}}
}

func TestStoreStartOwnedInputIsDeletedOnRemove(t *testing.T) {
	store := newStore(t)
	inputPath := writeInput(t, store)

	job, err := store.Start(ownedInput(inputPath), conversion.Spec{Muxer: conversion.PluginSpec{Name: "wav"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitDone(t, job)
	if job.Err() != nil {
		t.Fatalf("job.Err() = %v", job.Err())
	}

	if _, ok := store.Get(job.ID); !ok {
		t.Fatal("Get() did not find started job")
	}
	if _, err := os.Stat(job.OutputPath()); err != nil {
		t.Fatalf("output file missing: %v", err)
	}

	store.Remove(job.ID)
	if _, ok := store.Get(job.ID); ok {
		t.Fatal("Get() found job after Remove")
	}
	if _, err := os.Stat(inputPath); !os.IsNotExist(err) {
		t.Fatalf("owned input file was not deleted: err=%v", err)
	}
	if _, err := os.Stat(job.OutputPath()); !os.IsNotExist(err) {
		t.Fatalf("output file was not deleted: err=%v", err)
	}
}

func TestStoreStartUnownedInputSurvivesRemove(t *testing.T) {
	store := newStore(t)
	sharedPath := filepath.Join(t.TempDir(), "preset.wav")
	sharedFile, err := os.Create(sharedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := testutil.WriteWAV(sharedFile); err != nil {
		t.Fatal(err)
	}
	sharedFile.Close()

	job, err := store.Start(jobs.Inputs{Main: jobs.Input{Path: sharedPath}}, conversion.Spec{Muxer: conversion.PluginSpec{Name: "wav"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitDone(t, job)

	store.Remove(job.ID)
	if _, err := os.Stat(sharedPath); err != nil {
		t.Fatalf("unowned (preset) input was deleted: %v", err)
	}
}

func TestStoreRemovesEveryOwnedInput(t *testing.T) {
	store := newStore(t)
	mainPath := writeInput(t, store)
	impulsePath := writeInput(t, store)

	job, err := store.Start(jobs.Inputs{
		Main: jobs.Input{Path: mainPath, Owned: true},
		Aux:  map[string]jobs.Input{"ir": {Path: impulsePath, Owned: true}},
	}, conversion.Spec{
		Muxer: conversion.PluginSpec{Name: "wav"},
		Filters: []conversion.FilterSpec{{
			PluginSpec: conversion.PluginSpec{Name: "convolver"},
			Inputs:     map[string]conversion.PortRef{"ir": {Alias: "ir"}},
		}},
		AuxInputs: map[string]conversion.AuxInputSpec{"ir": {}},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitDone(t, job)
	if err := job.Err(); err != nil {
		t.Fatalf("job.Err() = %v", err)
	}

	store.Remove(job.ID)
	for _, path := range []string{mainPath, impulsePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("owned input %s was not deleted: %v", path, err)
		}
	}
}

func TestStoreCancel(t *testing.T) {
	store := newStore(t)
	inputPath := writeInput(t, store)

	job, err := store.Start(ownedInput(inputPath), conversion.Spec{Muxer: conversion.PluginSpec{Name: "wav"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	job.Cancel()
	waitDone(t, job)

	status := job.Snapshot().Status
	if status != conversion.StatusCanceled && status != conversion.StatusCompleted {
		t.Fatalf("snapshot.Status = %v, want %v or %v", status, conversion.StatusCanceled, conversion.StatusCompleted)
	}
	store.Remove(job.ID)
}

func TestStoreGetUnknownID(t *testing.T) {
	store := newStore(t)
	if _, ok := store.Get("nope"); ok {
		t.Fatal("Get() found a job for an unknown ID")
	}
}
