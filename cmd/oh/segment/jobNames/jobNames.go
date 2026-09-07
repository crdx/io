package jobNames

import (
	"strings"
	"time"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/jobs"
)

const beat = time.Second

const (
	liveMark     = "\u25cf"
	finishedMark = "\u25cb"
	failedMark   = "\u2717"
)

var _ segment.Refresher = state{}

type state struct {
	getJobs func() []jobs.Snapshot
}

func New(getJobs func() []jobs.Snapshot) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{getJobs: getJobs}, nil
	}
}

func (self state) Render(segment.Context) string {
	var marks []string

	for _, snapshot := range self.getJobs() {
		if mark := describe(snapshot); mark != "" {
			marks = append(marks, mark)
		}
	}

	return strings.Join(marks, " ")
}

func (self state) NextRefresh(segment.Phase) time.Time {
	for _, snapshot := range self.getJobs() {
		if snapshot.IsLive() {
			return time.Now().Add(beat)
		}
	}

	return time.Time{}
}

func describe(snapshot jobs.Snapshot) string {
	switch snapshot.State {
	case jobs.StateStarting, jobs.StateRunning:
		return style.Information(liveMark + " " + snapshot.Name)
	case jobs.StateStopping:
		return style.Change(liveMark + " " + snapshot.Name)
	case jobs.StateFailed:
		return style.Failure(failedMark + " " + snapshot.Name)
	case jobs.StateComplete, jobs.StateStopped, jobs.StateEnded:
		return style.Dim(finishedMark + " " + snapshot.Name)
	}

	return ""
}
