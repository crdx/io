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
	"crdx.org/io/cmd/oh/segment/cacheTTL"
	"crdx.org/io/cmd/oh/segment/cacheUsage"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/segment/exposedPorts"
	"crdx.org/io/cmd/oh/segment/fastMode"
	"crdx.org/io/cmd/oh/segment/gitBranch"
	"crdx.org/io/cmd/oh/segment/jobNames"
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
	"crdx.org/io/cmd/oh/usage"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/internal/jobs"
)

const (
	activitySpinnerSegment = "activity-spinner"
	cacheTTLSegment        = "cache-ttl"
	cacheUsageSegment      = "cache-usage"
	contextUsageSegment    = "context-usage"
	modeToggleSegment      = "mode-toggle"
	pathGrantsSegment      = "path-grants"
	exposedPortsSegment    = "exposed-ports"
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
	jobNamesSegment        = "jobs"
)

type Options struct {
	Workspace             *work.Space
	CurrentSessionName    string
	ModelName             string
	ModelEffort           string
	ModelEffortLevels     []string
	IsFast                bool
	UsageReporter         agent.UsageReporter
	UsageCachePath        string
	UsageIsSelfRefreshing bool
	UsageGauges           *usage.Gauges
	Sources               Sources
}

type Sources struct {
	IsTurnRunning   func() bool
	GetContextUsage func() (int, int)
	GetCacheUsage   func() (int, int)
	GetCacheLife    func() time.Duration
	GetGrantedCaps  func() caps.Set
	GetPathGrants   func() []pathgrant.Grant
	GetExposedPorts func() []uint16
	IsPrefixPending func() bool
	GetTurnTiming   func() turn.Timing
	GetTurnCount    func() int
	GetJobs         func() []jobs.Snapshot
}

func NewRegistry(options Options) segment.Registry {
	return segment.Registry{
		activitySpinnerSegment: activitySpinner.New(options.Sources.IsTurnRunning, time.Now),
		cacheTTLSegment:        cacheTTL.New(options.Sources.GetCacheLife),
		cacheUsageSegment:      cacheUsage.New(options.Sources.GetCacheUsage),
		contextUsageSegment:    contextUsage.New(options.Sources.GetContextUsage),
		modeToggleSegment:      modeToggle.New(options.Sources.GetGrantedCaps, options.Sources.IsPrefixPending),
		pathGrantsSegment:      pathGrants.New(options.Sources.GetPathGrants),
		exposedPortsSegment:    exposedPorts.New(options.Sources.GetExposedPorts),
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
		jobNamesSegment:       jobNames.New(options.Sources.GetJobs, time.Now),
		subUsageSegment: subUsage.New(subUsage.Settings{
			Reporter:         options.UsageReporter,
			CachePath:        options.UsageCachePath,
			ModelName:        options.ModelName,
			IsSelfRefreshing: options.UsageIsSelfRefreshing,
			Gauges:           options.UsageGauges,
			Now:              time.Now,
		}),
	}
}

var segmentSeparator = " " + style.Subtle("─") + " "

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
		instance = underlying(instance)
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

func underlying(instance segment.Segment) segment.Segment {
	if namedInstance, isNamed := instance.(segment.Instance); isNamed {
		return namedInstance.Segment
	}
	return instance
}

type Config struct {
	registry segment.Registry
	layout   segment.Layout
}

func NewConfiguration(registry segment.Registry, layout segment.Layout) Config {
	return Config{registry: registry, layout: layout}
}

func (self *Config) GetRegistry() segment.Registry {
	return self.registry
}

func (self *Config) ReplaceLayout(layout segment.Layout) {
	self.layout = layout
}

func (self *Config) RenderInfo(context segment.Context) string {
	type infoRow struct {
		name  string
		value string
	}

	var rows []infoRow
	nameCells := 0
	for _, position := range segment.Positions {
		for _, instance := range self.layout[position] {
			namedInstance, isNamed := instance.(segment.Instance)
			if !isNamed || !isInfoSegment(namedInstance.Name) {
				continue
			}
			value := namedInstance.Segment.Render(context)
			if value == "" {
				continue
			}
			rows = append(rows, infoRow{name: namedInstance.Name, value: value})
			nameCells = max(nameCells, style.Width(namedInstance.Name))
		}
	}

	drawnRows := make([]string, 0, len(rows))
	for _, row := range rows {
		padding := strings.Repeat(" ", nameCells-style.Width(row.name)+2)
		drawnRows = append(drawnRows, style.Information(row.name)+padding+row.value)
	}
	return strings.Join(drawnRows, "\n")
}

func isInfoSegment(name string) bool {
	return name != activitySpinnerSegment && name != scrollOverflowSegment
}

func (self *Config) Render(position segment.Position, context segment.Context) string {
	return Render(self.layout, position, context)
}

func (self *Config) RenderWithin(position segment.Position, context segment.Context, cells int) string {
	return RenderWithin(self.layout, position, context, cells)
}

func (self *Config) NextRefresh(phase segment.Phase) time.Time {
	return self.layout.NextRefresh(phase)
}
