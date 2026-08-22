package main

import (
	"strings"

	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activity"
	"crdx.org/io/cmd/oh/segment/model"
	"crdx.org/io/cmd/oh/segment/modes"
	"crdx.org/io/cmd/oh/segment/scroll"
	"crdx.org/io/cmd/oh/segment/think"
	"crdx.org/io/cmd/oh/segment/workdir"
	"crdx.org/io/cmd/oh/style"
)

const (
	activitySegment = "activity"
	modesSegment    = "modes"
	workdirSegment  = "workdir"
	modelSegment    = "model"
	thinkSegment    = "think"
	scrollSegment   = "scroll"
)

func availableSegments(
	workspaceDir string,
	modelName string,
	effort string,
	harness *Harness,
) segment.Set {
	return segment.Set{
		activitySegment: activity.New(harness.turnActivity),
		modesSegment:    modes.New(harness.grantedCaps, harness.isChordPending),
		workdirSegment:  workdir.New(workspaceDir),
		modelSegment:    model.New(modelName),
		thinkSegment:    think.New(effort),
		scrollSegment:   scroll.New,
	}
}

func bar(layout segment.Layout, position segment.Position, frame edit.Frame) string {
	drawn := make([]string, 0, len(layout[position]))

	context := segment.Context{Above: frame.Above, Below: frame.Below}

	for _, placed := range layout[position] {
		if text := placed.Render(context); style.Width(text) > 0 {
			drawn = append(drawn, text)
		}
	}

	return strings.Join(drawn, " "+style.Subtle("\u2500")+" ")
}
