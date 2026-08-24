package main

import (
	"strings"
	"time"

	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activeModel"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/currentSession"
	"crdx.org/io/cmd/oh/segment/currentTime"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/lastTps"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/segment/turnElapsed"
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
	currentSessionSegment   = "current-session"
	currentTimeSegment      = "current-time"
	turnElapsedSegment      = "turn-elapsed"
	turnCountSegment        = "turn-count"
	lastTpsSegment          = "last-tps"
	gitBranchSegment        = "git-branch"
)

func availableSegments(
	workspaceDir string,
	currentSessionName string,
	modelName string,
	modelEffort string,
	harness *Harness,
) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment:  activitySpinner.New(harness.turnActivity),
		contextUsageSegment:     contextUsage.New(harness.contextUsage),
		modeToggleSegment:       modeToggle.New(harness.grantedCaps, harness.isPrefixPending),
		workingDirectorySegment: workingDirectory.New(workspaceDir),
		activeModelSegment:      activeModel.New(modelName, modelEffort),
		scrollOverflowSegment:   scrollOverflow.New,
		currentSessionSegment:   currentSession.New(currentSessionName),
		currentTimeSegment:      currentTime.New(time.Now),
		turnElapsedSegment:      turnElapsed.New(harness.turnElapsed),
		turnCountSegment:        turnCount.New(harness.turnCount),
		lastTpsSegment:          lastTps.New(harness.lastTurnTokenRate, harness.isTurnRunning),
		gitBranchSegment:        gitBranch.New(workspaceDir),
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
