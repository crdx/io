// Package status draws the rows a set of tool calls is shown on while they run.
package status

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"crdx.org/io/cmd/oh/markdown"
	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/theme"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

const (
	reveal           = time.Second
	durationWidth    = 6
	patience         = 5 * time.Second
	bytesPerMegabyte = 1 << 20
)

// State is a call.
type State int

// What a row can say about a call: it is running, it finished, it failed, or it never got anywhere.
const (
	Running State = iota
	Done
	Failed
	Cancelled
)

// Label is what a row says: the name of a call, the arguments it was made with, and whatever
// qualifies those.
type Label struct {
	Name        string      // the call name
	Args        string      // the rendered arguments
	Focus       string      // the part of the arguments set apart from the rest
	Syntax      string      // the language the arguments are written in
	Detail      string      // what qualifies the arguments
	ReadOnly    bool        // whether the call changes nothing, which decides the colour its name is in
	NameStyle   theme.Style // an explicit style for a tool with its own prompt
	Accent      string      // another part of the arguments set apart from the rest
	AccentStyle theme.Style // how the accent is painted
}

// Elide cuts a label to the room it has, so the row stays on the line it was printed on. What
// qualifies the arguments is the first to go, being the least of it.
func (self Label) Elide(room int) Label {
	self.Name = elide(self.Name, room)
	room -= width.Of(self.Name) + 1

	if room > 0 {
		self.Args = elide(self.Args, room)
		room -= width.Of(self.Args) + 1
	} else {
		self.Args = ""
		room = 0
	}

	if room > 0 {
		self.Detail = elide(self.Detail, room)
	} else {
		self.Detail = ""
	}

	return self
}

func (self Label) render() string {
	line := self.style()(self.Name)

	if self.Args != "" {
		line += " " + self.renderArgs()
	}

	if self.Detail != "" {
		line += " " + self.renderDetail()
	}

	return line
}

func (self Label) renderArgs() string {
	if self.Syntax != "" {
		return markdown.Highlight(self.Args, self.Syntax)
	}

	type span struct {
		start int
		end   int
		style theme.Style
	}

	spans := []span{}
	if at := strings.LastIndex(self.Args, self.Focus); self.Focus != "" && at >= 0 {
		spans = append(spans, span{start: at, end: at + len(self.Focus), style: theme.Args})
	}
	if at := strings.LastIndex(self.Args, self.Accent); self.Accent != "" && self.AccentStyle != nil && at >= 0 {
		spans = append(spans, span{start: at, end: at + len(self.Accent), style: self.AccentStyle})
	}

	if len(spans) == 0 {
		return theme.Args(self.Args)
	}

	sort.Slice(spans, func(i int, j int) bool { return spans[i].start < spans[j].start })

	var out strings.Builder
	at := 0
	for _, marked := range spans {
		if marked.start < at {
			continue
		}
		out.WriteString(theme.Detail(self.Args[at:marked.start]))
		out.WriteString(marked.style(self.Args[marked.start:marked.end]))
		at = marked.end
	}
	if at < len(self.Args) {
		out.WriteString(theme.Detail(self.Args[at:]))
	}

	return out.String()
}

func (self Label) renderDetail() string {
	if self.Focus == "" || strings.Contains(self.Args, self.Focus) {
		return theme.Detail(self.Detail)
	}

	at := strings.LastIndex(self.Detail, self.Focus)
	if at < 0 {
		return theme.Detail(self.Detail)
	}

	end := at + len(self.Focus)

	var out strings.Builder
	if at > 0 {
		out.WriteString(theme.Detail(self.Detail[:at]))
	}
	out.WriteString(theme.Args(self.Detail[at:end]))
	if end < len(self.Detail) {
		out.WriteString(theme.Detail(self.Detail[end:]))
	}

	return out.String()
}

func (self Label) width() int {
	total := width.Of(self.Name)

	if self.Args != "" {
		total += 1 + width.Of(self.Args)
	}

	if self.Detail != "" {
		total += 1 + width.Of(self.Detail)
	}

	return total
}

func (self Label) style() theme.Style {
	if self.NameStyle != nil {
		return self.NameStyle
	}
	if self.ReadOnly {
		return theme.Call
	}

	return theme.Change
}

const ellipsis = "…"

func elide(text string, room int) string {
	if room <= 0 {
		return ""
	}

	if width.Of(text) <= room {
		return text
	}

	if room == 1 {
		return ellipsis
	}

	nonEmptyParts, _ := width.Cut(text, room-1)

	return nonEmptyParts + ellipsis
}

type row struct {
	label     Label            // what the row says
	state     State            // how the call ended
	startedAt time.Time        // when the call began
	took      time.Duration    // how long the call took
	failure   string           // why it ended that way, where that was a failure
	stats     *tool.Statistics // resources or sizes measured by the tool
}

// Block displays and redraws a group of tool-call rows. Nothing else may print until it closes.
type Block struct {
	print   func(string)      // prints a new row
	overlay func(string, int) // redraws existing rows
	live    bool              // whether rows may be redrawn
	columns int               // the width available

	mutex    sync.Mutex // guards the changing rows
	rows     []row      // the calls being shown
	frame    int        // the spinner frame
	revealed bool       // whether elapsed times are shown

	stop     chan struct{}  // asks the ticker to stop
	stopWait sync.WaitGroup // waits for the ticker to end
}

// New opens an empty block. Non-live blocks ignore redraws.
func New(print func(string), overlay func(string, int), live bool, columns int) *Block {
	self := &Block{
		print:   print,
		overlay: overlay,
		live:    live,
		columns: columns,
		stop:    make(chan struct{}),
	}

	self.stopWait.Add(1)

	go self.run()

	return self
}

// Add puts a call on the block and hands back the row it went on.
func (self *Block) Add(label Label) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.rows = append(self.rows, row{label: label, startedAt: time.Now()})

	index := len(self.rows) - 1

	if index > 0 {
		self.print("\n")
	}

	self.print(self.line(self.rows[index]))

	return index
}

// Mark completes a call and removes its spinner. Failure text is flattened to one line.
func (self *Block) Mark(index int, state State, took time.Duration, reason string) {
	self.MarkWithStats(index, state, took, reason, nil)
}

// MarkWithStats marks a call and includes measurements made by its tool.
func (self *Block) MarkWithStats(
	index int,
	state State,
	took time.Duration,
	reason string,
	stats *tool.Statistics,
) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if index < 0 || index >= len(self.rows) || self.rows[index].state != Running {
		return
	}

	self.rows[index].state = state
	self.rows[index].took = took
	self.rows[index].stats = stats

	if state == Failed {
		self.rows[index].failure = collapse(reason)
	}

	self.redraw()
}

func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// Stop ends the block where it stands, saying nothing more about it. What was drawn is left on the
// screen for whatever is about to take its place.
func (self *Block) Stop() {
	close(self.stop)
	self.stopWait.Wait()
}

// Close ends the block, marking anything still running with what became of it.
func (self *Block) Close(state State) {
	self.Stop()

	self.mutex.Lock()
	defer self.mutex.Unlock()

	for index := range self.rows {
		if self.rows[index].state == Running {
			self.rows[index].state = state
			self.rows[index].took = time.Since(self.rows[index].startedAt)
		}
	}

	self.revealed = false

	self.redraw()
}

func (self *Block) run() {
	defer self.stopWait.Done()

	select {
	case <-self.stop:
		return
	case <-time.After(reveal):
	}

	self.mutex.Lock()
	self.revealed = true
	self.redraw()
	self.mutex.Unlock()

	ticker := time.NewTicker(spinner.Activity.Rate())
	defer ticker.Stop()

	for {
		select {
		case <-self.stop:
			return
		case <-ticker.C:
			self.mutex.Lock()
			self.frame++
			self.redraw()
			self.mutex.Unlock()
		}
	}
}

func (self *Block) redraw() {
	if !self.live || len(self.rows) == 0 {
		return
	}

	lines := make([]string, len(self.rows))

	for index, item := range self.rows {
		lines[index] = "\r\x1b[K" + self.line(item)
	}

	up := ""
	if len(self.rows) > 1 {
		up = fmt.Sprintf("\x1b[%dA", len(self.rows)-1)
	}

	self.overlay(up+strings.Join(lines, "\n"), theme.Width(lines[len(lines)-1]))
}

const (
	failureShare = 2 // of the row, as a floor and not a ceiling
	edgeGuard    = 2
)

func (self *Block) line(item row) string {
	outcome := self.outcome(item)

	label := item.label
	failure := item.failure

	if self.columns > 0 {
		room := self.columns - edgeGuard - theme.Width(outcome) - outcomeSpacing(outcome)

		if failure != "" {
			spare := max(room-label.width()-1, room/failureShare)
			failure = elide(failure, min(width.Of(failure), spare))
			room -= width.Of(failure) + 1
		}

		label = label.Elide(room)
	}

	return util.JoinNonEmpty(label.render(), outcome, failureText(failure))
}

func failureText(failure string) string {
	if failure == "" {
		return ""
	}

	return theme.Failure(failure)
}

func outcomeSpacing(outcome string) int {
	if outcome == "" {
		return 0
	}

	return 1
}

func (self *Block) outcome(item row) string {
	if item.state == Running {
		if !self.revealed {
			return ""
		}

		return outcomeText(theme.Spinner(spinner.Activity.Frame(self.frame)), time.Since(item.startedAt), nil)
	}

	return outcomeText(glyph(item.state), item.took, item.stats)
}

func outcomeText(mark string, took time.Duration, stats *tool.Statistics) string {
	if stats == nil {
		if took < patience {
			return mark
		}
		return mark + " " + theme.Spinner(util.FormatDuration(took))
	}

	var statsText string
	switch stats.Kind {
	case tool.StatsResources:
		statsText = theme.Detail(fmt.Sprintf(
			"%s %dL %s %s %dM",
			tokenEstimate(stats),
			stats.Lines,
			util.CompactDuration(took),
			util.CompactDuration(stats.CPUTime),
			stats.PeakMemory/bytesPerMegabyte,
		))
	case tool.StatsRead:
		statsText = theme.Detail(fmt.Sprint(stats.Lines) + "L " + tokenEstimate(stats))
	case tool.StatsList:
		statsText = theme.Detail(fmt.Sprint(stats.Lines) + "L")
	case tool.StatsImage:
		statsText = theme.Detail(util.FormatEstimatedTokenCount(stats.EstimatedTokens, 2))
	case tool.StatsWrite:
		statsText = theme.Detail(fmt.Sprint(stats.Lines) + "L " + tokenEstimate(stats))
	case tool.StatsDiff:
		statsText = theme.Success("+%d", stats.Added) +
			theme.Detail(" ") + theme.Failure("−%d", stats.Removed)
	case tool.StatsSearch:
		capMarker := ""
		if stats.Truncated {
			capMarker = "+"
		}
		statsText = theme.Detail(fmt.Sprintf("%dL%s %s", stats.Lines, capMarker, tokenEstimate(stats)))
	}

	if statsText == "" {
		return mark
	}
	return mark + " " + statsText
}

func tokenEstimate(stats *tool.Statistics) string {
	returned := util.FormatTokenEstimate(stats.Bytes, 2)
	if stats.TotalBytes > stats.Bytes {
		return returned + " (of " + util.FormatTokenEstimate(stats.TotalBytes, 2) + ")"
	}
	return returned
}

func glyph(state State) string {
	switch state {
	case Failed:
		return theme.Failure("✗")
	case Cancelled:
		return theme.Cancelled("–")
	case Done, Running:
		return theme.Success("✓")
	}

	return ""
}
