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
	"crdx.org/io/cmd/oh/segment/localTime"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/sessionEmoji"
	"crdx.org/io/cmd/oh/segment/sessionName"
	"crdx.org/io/cmd/oh/segment/subUsage"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/segment/turnTimer"
	"crdx.org/io/cmd/oh/segment/workspaceDir"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/turn"
)

const (
	activitySpinnerSegment = "activity-spinner"
	contextUsageSegment    = "context-usage"
	modeToggleSegment      = "mode-toggle"
	workspaceDirSegment    = "workspace-dir"
	activeModelSegment     = "active-model"
	scrollOverflowSegment  = "scroll-overflow"
	sessionNameSegment     = "session-name"
	sessionEmojiSegment    = "session-emoji"
	localTimeSegment       = "local-time"
	turnTimerSegment       = "turn-timer"
	turnCountSegment       = "turn-count"
	gitBranchSegment       = "git-branch"
	subUsageSegment        = "subscription-usage"
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
	IsTurnRunning   func() bool
	GetContextUsage func() (int, int)
	GetGrantedCaps  func() caps.Set
	IsPrefixPending func() bool
	GetTurnTiming   func() turn.Timing
	GetTurnCount    func() int
}

func NewRegistry(options Options) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment: activitySpinner.New(options.Sources.IsTurnRunning, time.Now),
		contextUsageSegment:    contextUsage.New(options.Sources.GetContextUsage),
		modeToggleSegment:      modeToggle.New(options.Sources.GetGrantedCaps, options.Sources.IsPrefixPending),
		workspaceDirSegment:    workspaceDir.New(options.WorkspaceDir),
		activeModelSegment:     activeModel.New(options.ModelName, options.ModelEffort),
		scrollOverflowSegment:  scrollOverflow.New,
		sessionNameSegment:     sessionName.New(options.CurrentSessionName),
		sessionEmojiSegment:    sessionEmoji.New(options.CurrentSessionName),
		localTimeSegment:       localTime.New(time.Now),
		turnTimerSegment:       turnTimer.New(options.Sources.GetTurnTiming, options.Sources.IsTurnRunning),
		turnCountSegment:       turnCount.New(options.Sources.GetTurnCount),
		gitBranchSegment:       gitBranch.New(options.WorkspaceDir),
		subUsageSegment:        subUsage.New(options.UsageReporter, options.UsageCachePath, options.ModelName, time.Now),
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
