package main

import (
	"strings"

	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activeModel"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/workingDirectory"
	"crdx.org/io/cmd/oh/style"
)

const (
	activitySpinnerSegment  = "activity-spinner"
	contextUsageSegment     = "context-usage"
	modeToggleSegment       = "mode-toggle"
	workingDirectorySegment = "working-directory"
	activeModelSegment      = "active-model"
	scrollOverflowSegment   = "scroll-overflow"
)

func availableSegments(
	workspaceDir string,
	modelName string,
	modelEffort string,
	harness *Harness,
) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment:  activitySpinner.New(harness.turnActivity),
		contextUsageSegment:     contextUsage.New(harness.contextUsage),
		modeToggleSegment:       modeToggle.New(harness.grantedCaps, harness.isChordPending),
		workingDirectorySegment: workingDirectory.New(workspaceDir),
		activeModelSegment:      activeModel.New(modelName, modelEffort),
		scrollOverflowSegment:   scrollOverflow.New,
	}
}

func bar(layout segment.Layout, position segment.Position, frame edit.Frame) string {
	drawn := make([]string, 0, len(layout[position]))

	context := segment.Context{
		HiddenLinesAbove: frame.HiddenLinesAbove,
		HiddenLinesBelow: frame.HiddenLinesBelow,
	}

	for _, instance := range layout[position] {
		if text := instance.Render(context); style.Width(text) > 0 {
			drawn = append(drawn, text)
		}
	}

	return strings.Join(drawn, " "+style.Subtle("\u2500")+" ")
}
