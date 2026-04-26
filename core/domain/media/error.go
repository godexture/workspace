package media

import (
	"fmt"
)

type PiplineStage string

const (
	StageDemuxer PiplineStage = "demuxer"
	StageDecoder PiplineStage = "decoder"
	StageFilter  PiplineStage = "filter"
	StageEncoder PiplineStage = "encoder"
	StageMuxer   PiplineStage = "muxer"
)

type Error struct {
	PTS         Pts
	Stage       PiplineStage
	StreamIndex int
	Err         error
}

func (e *Error) Error() string {
	return fmt.Sprintf("[%s] stream=%d, pts=%d: %v", e.Stage, e.StreamIndex, e.PTS, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

type ErrorAction int

const (
	ActionStop ErrorAction = iota
	ActionIgnore
)

type ErrorHandler interface {
	Handle(err *Error) ErrorAction
}
