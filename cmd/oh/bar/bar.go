package bar

import (
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activeModel"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/lastTps"
	"crdx.org/io/cmd/oh/segment/localTime"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/sessionName"
	"crdx.org/io/cmd/oh/segment/subUsage"
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
	sessionNameSegment      = "session-name"
	localTimeSegment        = "local-time"
	turnElapsedSegment      = "turn-elapsed"
	turnCountSegment        = "turn-count"
	lastTpsSegment          = "last-tps"
	gitBranchSegment        = "git-branch"
	subUsageSegment         = "subscription-usage"
)

type Options struct {
	WorkspaceDir       string
	CurrentSessionName string
	ModelName          string
	ModelEffort        string
	UsageReporter      agent.UsageReporter
	UsageCachePath     string
	Sources            Sources
}

type Sources struct {
	GetTurnActivity      func() (bool, int)
	GetContextUsage      func() (int, int)
	GetGrantedCaps       func() caps.Set
	IsPrefixPending      func() bool
	GetTurnElapsed       func() (bool, time.Duration, bool)
	GetTurnCount         func() int
	GetLastTurnTokenRate func() (float64, bool)
	IsTurnRunning        func() bool
}

func NewRegistry(options Options) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment:  activitySpinner.New(options.Sources.GetTurnActivity),
		contextUsageSegment:     contextUsage.New(options.Sources.GetContextUsage),
		modeToggleSegment:       modeToggle.New(options.Sources.GetGrantedCaps, options.Sources.IsPrefixPending),
		workingDirectorySegment: workingDirectory.New(options.WorkspaceDir),
		activeModelSegment:      activeModel.New(options.ModelName, options.ModelEffort),
		scrollOverflowSegment:   scrollOverflow.New,
		sessionNameSegment:      sessionName.New(options.CurrentSessionName),
		localTimeSegment:        localTime.New(time.Now),
		turnElapsedSegment:      turnElapsed.New(options.Sources.GetTurnElapsed),
		turnCountSegment:        turnCount.New(options.Sources.GetTurnCount),
		lastTpsSegment:          lastTps.New(options.Sources.GetLastTurnTokenRate, options.Sources.IsTurnRunning),
		gitBranchSegment:        gitBranch.New(options.WorkspaceDir),
		subUsageSegment:         subUsage.New(options.UsageReporter, options.UsageCachePath, options.ModelName, time.Now),
	}
}

func Render(layout segment.Layout, position segment.Position, context segment.Context) string {
	drawn := make([]string, 0, len(layout[position]))

	for _, instance := range layout[position] {
		if text := instance.Render(context); style.Width(text) > 0 {
			drawn = append(drawn, text)
		}
	}

	return strings.Join(drawn, " "+style.Subtle("\u2500")+" ")
}
