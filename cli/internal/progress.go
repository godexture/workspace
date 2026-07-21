package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/godexture/core/pipeline"
)

type progressConfig struct {
	enabled  bool
	terminal bool
	interval time.Duration
}

func resolveProgressConfig(value string, terminal, dryRun bool) (progressConfig, error) {
	if value != "auto" && value != "always" && value != "never" {
		return progressConfig{}, fmt.Errorf("invalid progress mode %q; use auto, always, or never", value)
	}
	if dryRun {
		return progressConfig{}, nil
	}
	switch value {
	case "auto":
		if !terminal {
			return progressConfig{}, nil
		}
		return progressConfig{enabled: true, terminal: true, interval: 250 * time.Millisecond}, nil
	case "always":
		interval := time.Second
		if terminal {
			interval = 250 * time.Millisecond
		}
		return progressConfig{enabled: true, terminal: terminal, interval: interval}, nil
	case "never":
		return progressConfig{}, nil
	}
	return progressConfig{}, nil
}

type progressReporter struct {
	writer    io.Writer
	pipeline  *pipeline.Pipeline
	input     *measuredReadSeeker
	config    progressConfig
	startedAt time.Time
	stop      chan bool
	done      chan struct{}
	lastWidth int
}

func startProgressReporter(writer io.Writer, conversion *pipeline.Pipeline, input *measuredReadSeeker, config progressConfig) *progressReporter {
	reporter := &progressReporter{
		writer: writer, pipeline: conversion, input: input, config: config,
		startedAt: time.Now(), stop: make(chan bool, 1), done: make(chan struct{}),
	}
	go reporter.run()
	return reporter
}

func (reporter *progressReporter) run() {
	defer close(reporter.done)
	ticker := time.NewTicker(reporter.config.interval)
	defer ticker.Stop()
	reporter.render(false, false)
	for {
		select {
		case <-ticker.C:
			reporter.render(false, false)
		case success := <-reporter.stop:
			reporter.render(true, success)
			return
		}
	}
}

func (reporter *progressReporter) Stop(success bool) {
	reporter.stop <- success
	<-reporter.done
}

func (reporter *progressReporter) render(final, success bool) {
	line := formatProgress(reporter.pipeline.Snapshot(), reporter.input.Snapshot(), time.Since(reporter.startedAt), success)
	if reporter.config.terminal {
		padding := ""
		if reporter.lastWidth > len(line) {
			padding = strings.Repeat(" ", reporter.lastWidth-len(line))
		}
		_, _ = fmt.Fprintf(reporter.writer, "\r%s%s", line, padding)
		reporter.lastWidth = len(line)
		if final {
			_, _ = fmt.Fprintln(reporter.writer)
		}
		return
	}
	_, _ = fmt.Fprintln(reporter.writer, line)
}

func formatProgress(snapshot pipeline.Snapshot, input inputMetrics, elapsed time.Duration, success bool) string {
	for _, edge := range snapshot.Edges {
		if !edge.Description.ProgressSource {
			continue
		}
		duration := edge.Description.Stream.Duration
		if duration > 0 && (edge.MediaTime > 0 || success) {
			processed := edge.MediaTime
			if success {
				processed = duration
			}
			processed = min(processed, duration)
			percent := float64(processed) / float64(duration) * 100
			speed := 0.0
			if elapsed > 0 {
				speed = float64(processed) / float64(elapsed)
			}
			eta := time.Duration(0)
			if speed > 0 && processed < duration {
				eta = time.Duration(float64(duration-processed) / speed)
			}
			return fmt.Sprintf("%6.2f%%  %s / %s  %.2fx  elapsed %s  eta %s",
				percent, formatElapsed(processed), formatElapsed(duration), speed, formatElapsed(elapsed), formatElapsed(eta))
		}
		break
	}
	if input.Size > 0 {
		position := max(int64(0), min(input.Position, input.Size))
		if success {
			position = input.Size
		}
		percent := float64(position) / float64(input.Size) * 100
		rate := 0.0
		if elapsed > 0 {
			rate = float64(position) / elapsed.Seconds()
		}
		eta := time.Duration(0)
		if rate > 0 && position < input.Size {
			eta = time.Duration(float64(input.Size-position) / rate * float64(time.Second))
		}
		return fmt.Sprintf("%6.2f%%  %s / %s  %s/s  elapsed %s  eta %s",
			percent, formatBytes(uint64(position)), formatBytes(uint64(input.Size)), formatBytes(uint64(rate)), formatElapsed(elapsed), formatElapsed(eta))
	}
	var items uint64
	for _, edge := range snapshot.Edges {
		if edge.Description.ProgressSource {
			items = edge.Items
			break
		}
	}
	return fmt.Sprintf("processed %d items  elapsed %s", items, formatElapsed(elapsed))
}

func isTerminalWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	return ok && terminalFile(file)
}
