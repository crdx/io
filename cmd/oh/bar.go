package main

import (
	"strings"

	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activity"
	"crdx.org/io/cmd/oh/segment/model"
	"crdx.org/io/cmd/oh/segment/modes"
	"crdx.org/io/cmd/oh/segment/scroll"
	"crdx.org/io/cmd/oh/segment/workdir"
	"crdx.org/io/cmd/oh/style"
)

const (
	activitySegment = "activity"
	modesSegment    = "modes"
	workdirSegment  = "workdir"
	modelSegment    = "model"
	scrollSegment   = "scroll"
)

func availableSegments(
	workspaceDir string,
	modelName string,
	modelEffort string,
	harness *Harness,
) segment.Registry {
	return segment.Registry{
		activitySegment: activity.New(harness.turnActivity),
		modesSegment:    modes.New(harness.grantedCaps, harness.isChordPending),
		workdirSegment:  workdir.New(workspaceDir),
		modelSegment:    model.New(modelName, modelEffort),
		scrollSegment:   scroll.New,
	}
}

func bar(layout segment.Layout, position segment.Position, frame edit.Frame) string {
	drawn := make([]string, 0, len(layout[position]))

	context := segment.Context{
		HiddenLinesAbove: frame.Above,
		HiddenLinesBelow: frame.Below,
	}

	for _, instance := range layout[position] {
		if text := instance.Render(context); style.Width(text) > 0 {
			drawn = append(drawn, text)
		}
	}

	return strings.Join(drawn, " "+style.Subtle("\u2500")+" ")
}
