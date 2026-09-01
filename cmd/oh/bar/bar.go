package bar

import (
	"strings"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/activeModel"
	"crdx.org/io/cmd/oh/segment/activitySpinner"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/fastMode"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/localTime"
	"crdx.org/io/cmd/oh/segment/modeToggle"
	"crdx.org/io/cmd/oh/segment/pathGrants"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/sessionEmoji"
	"crdx.org/io/cmd/oh/segment/sessionName"
	"crdx.org/io/cmd/oh/segment/subUsage"
	"crdx.org/io/cmd/oh/segment/turnCount"
	"crdx.org/io/cmd/oh/segment/turnTimer"
	"crdx.org/io/cmd/oh/segment/workspaceDir"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/turn"
	"crdx.org/io/cmd/oh/work"
)

const (
	activitySpinnerSegment = "activity-spinner"
	contextUsageSegment    = "context-usage"
	modeToggleSegment      = "mode-toggle"
	pathGrantsSegment      = "path-grants"
	workspaceDirSegment    = "workspace-dir"
	activeModelSegment     = "active-model"
	fastModeSegment        = "fast-mode"
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
	Workspace          *work.Space
	CurrentSessionName string
	ModelName          string
	ModelEffort        string
	ModelEffortLevels  []string
	IsFast             bool
	UsageReporter      agent.UsageReporter
	UsageCachePath     string
	Sources            Sources
}

type Sources struct {
	IsTurnRunning   func() bool
	GetContextUsage func() (int, int)
	GetGrantedCaps  func() caps.Set
	GetPathGrants   func() []pathgrant.Grant
	IsPrefixPending func() bool
	GetTurnTiming   func() turn.Timing
	GetTurnCount    func() int
}

func NewRegistry(options Options) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment: activitySpinner.New(options.Sources.IsTurnRunning, time.Now),
		contextUsageSegment:    contextUsage.New(options.Sources.GetContextUsage),
		modeToggleSegment:      modeToggle.New(options.Sources.GetGrantedCaps, options.Sources.IsPrefixPending),
		pathGrantsSegment:      pathGrants.New(options.Sources.GetPathGrants),
		workspaceDirSegment:    workspaceDir.New(options.Workspace),
		activeModelSegment: activeModel.New(
			options.ModelName, options.ModelEffort, options.ModelEffortLevels, options.IsFast,
		),
		fastModeSegment:       fastMode.New(options.IsFast),
		scrollOverflowSegment: scrollOverflow.New,
		sessionNameSegment:    sessionName.New(options.CurrentSessionName),
		sessionEmojiSegment:   sessionEmoji.New(options.CurrentSessionName),
		localTimeSegment:      localTime.New(time.Now),
		turnTimerSegment:      turnTimer.New(options.Sources.GetTurnTiming, options.Sources.IsTurnRunning),
		turnCountSegment:      turnCount.New(options.Sources.GetTurnCount),
		gitBranchSegment:      gitBranch.New(options.Workspace.GetDir()),
		subUsageSegment:       subUsage.New(options.UsageReporter, options.UsageCachePath, options.ModelName, time.Now),
	}
}

var segmentSeparator = " " + style.Subtle("\u2500") + " "

func Render(layout segment.Layout, position segment.Position, context segment.Context) string {
	return render(layout, position, context, -1)
}

func RenderWithin(layout segment.Layout, position segment.Position, context segment.Context, cells int) string {
	return render(layout, position, context, max(cells, 0))
}

func render(layout segment.Layout, position segment.Position, context segment.Context, cells int) string {
	drawnSegments := make([]string, 0, len(layout[position]))
	usedCells := 0

	for _, instance := range layout[position] {
		separatorCells := 0
		if len(drawnSegments) > 0 {
			separatorCells = style.Width(segmentSeparator)
		}

		var text string
		if fitter, isFitter := instance.(segment.Fitter); isFitter && cells >= 0 {
			text = fitter.RenderWithin(context, max(cells-usedCells-separatorCells, 0))
		} else {
			text = instance.Render(context)
		}
		textCells := style.Width(text)
		if textCells == 0 {
			continue
		}
		if cells >= 0 && usedCells+separatorCells+textCells > cells {
			break
		}

		drawnSegments = append(drawnSegments, text)
		usedCells += separatorCells + textCells
	}

	return strings.Join(drawnSegments, segmentSeparator)
}

type Configuration struct {
	registry segment.Registry
	layout   segment.Layout
}

func NewConfiguration(registry segment.Registry, layout segment.Layout) Configuration {
	return Configuration{registry: registry, layout: layout}
}

func (self *Configuration) GetRegistry() segment.Registry {
	return self.registry
}

func (self *Configuration) ReplaceLayout(layout segment.Layout) {
	self.layout = layout
}

func (self *Configuration) Render(position segment.Position, context segment.Context) string {
	return Render(self.layout, position, context)
}

func (self *Configuration) RenderWithin(position segment.Position, context segment.Context, cells int) string {
	return RenderWithin(self.layout, position, context, cells)
}

func (self *Configuration) NextRefresh(phase segment.Phase) time.Time {
	return self.layout.NextRefresh(phase)
}
