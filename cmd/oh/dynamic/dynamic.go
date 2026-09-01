package dynamic

import (
	"strings"
	"sync"
	"time"

	"crdx.org/io/cmd/oh/spinner"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
)

const (
	reveal   = time.Second
	patience = 5 * time.Second
)

type Label interface {
	Elide(room int) Label
	Render() string
	Width() int
}

type RowState int

const (
	Running RowState = iota
	Done
	Failed
	Cancelled
)

type row struct {
	label     Label
	state     RowState
	startedAt time.Time

	timeTaken time.Duration
	summary   string
	stats     string
}

type Block struct {
	refresh      func()
	mutex        sync.Mutex
	rows         []row
	spinnerFrame int
	isSlow       bool
	stop         chan struct{}
	stopWait     sync.WaitGroup
}

func NewBlock(refresh func()) *Block {
	self := &Block{
		refresh: refresh,
		stop:    make(chan struct{}),
	}

	self.stopWait.Add(1)

	go self.run()

	return self
}

func (self *Block) Add(label Label) int {
	index := 0

	self.change(func() {
		self.rows = append(self.rows, row{label: label, startedAt: time.Now()})
		index = len(self.rows) - 1
	})

	return index
}

func (self *Block) Rows(columns int) []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	rows := make([]string, len(self.rows))

	for at, item := range self.rows {
		rows[at] = self.line(item, columns)
	}

	return rows
}

func (self *Block) FinaliseRow(
	rowIndex int,
	state RowState,
	timeTaken time.Duration,
	summary string,
	stats string,
) {
	self.change(func() {
		if rowIndex < 0 || rowIndex >= len(self.rows) || self.rows[rowIndex].state != Running {
			return
		}

		self.rows[rowIndex].state = state
		self.rows[rowIndex].timeTaken = timeTaken
		self.rows[rowIndex].stats = stats
		self.rows[rowIndex].summary = summarise(summary)
	})
}

const widestSummaryRead = 1024

func summarise(text string) string {
	if len(text) > widestSummaryRead {
		text = strings.ToValidUTF8(text[:widestSummaryRead], "")
	}

	return strutil.Flatten(text)
}

func (self *Block) Stop() {
	close(self.stop)
	self.stopWait.Wait()
}

func (self *Block) Close(state RowState) {
	self.Stop()

	self.change(func() {
		for i := range self.rows {
			if self.rows[i].state == Running {
				self.rows[i].state = state
				self.rows[i].timeTaken = time.Since(self.rows[i].startedAt)
			}
		}

		self.isSlow = false
	})
}

func (self *Block) change(mutate func()) {
	self.mutex.Lock()
	mutate()
	self.mutex.Unlock()

	self.refresh()
}

func (self *Block) run() {
	defer self.stopWait.Done()

	select {
	case <-self.stop:
		return
	case <-time.After(reveal):
	}

	self.change(func() { self.isSlow = true })

	ticker := time.NewTicker(spinner.Activity.RefreshInterval())
	defer ticker.Stop()

	for {
		select {
		case <-self.stop:
			return
		case <-ticker.C:
			self.change(func() { self.spinnerFrame++ })
		}
	}
}

const (
	failureShare = 2
	edgeGuard    = 2
)

func (self *Block) line(row row, columns int) string {
	result := self.fitResult(row, columns)

	label := row.label
	summary := row.summary

	if columns > 0 {
		room := columns - edgeGuard - style.Width(result) - resultSpacing(result)

		summary = width.Elide(summary, summaryRoom(row.state, room, label.Width()))

		if summary != "" {
			room -= width.Of(summary) + 1
		}

		label = label.Elide(room)
	}

	return util.JoinNonEmpty(label.Render(), result, summaryText(row.state, summary))
}

func summaryRoom(state RowState, room int, labelWidth int) int {
	spare := room - labelWidth - 1

	if state == Failed || state == Cancelled {
		return max(spare, room/failureShare)
	}

	return spare
}

func summaryText(state RowState, summary string) string {
	if summary == "" {
		return ""
	}

	if state == Failed {
		return style.Failure(summary)
	}

	return style.Subtle(summary)
}

func resultSpacing(result string) int {
	if result == "" {
		return 0
	}

	return 1
}

func (self *Block) fitResult(row row, columns int) string {
	result := self.getResult(row)

	if columns <= 0 || style.Width(result)+edgeGuard <= columns {
		return result
	}

	if mark := self.getProgressIndicator(row); style.Width(mark) <= columns {
		return mark
	}

	return ""
}

func (self *Block) getResult(row row) string {
	if row.state == Running {
		elapsedTime := time.Since(row.startedAt).Truncate(time.Second)
		return getResultText(self.getProgressIndicator(row), elapsedTime, "")
	}

	return getResultText(self.getProgressIndicator(row), row.timeTaken, row.stats)
}

func (self *Block) getProgressIndicator(row row) string {
	if row.state != Running {
		return glyph(row.state)
	}

	if !self.isSlow {
		return ""
	}

	return style.Spinner(spinner.Activity.Frame(self.spinnerFrame))
}

func getResultText(mark string, took time.Duration, measuredText string) string {
	waitedText := ""
	if took >= patience {
		waitedText = style.Spinner(util.CompactDuration(took))
	}

	return util.JoinNonEmpty(mark, waitedText, measuredText)
}

func glyph(state RowState) string {
	switch state {
	case Failed:
		return style.Failure("✗")
	case Cancelled:
		return style.CancelledCall("–")
	case Done, Running:
		return style.Success("✓")
	}

	return ""
}
