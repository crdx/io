package painter

import (
	"strings"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/internal/util/strutil"
)

const (
	liveTextFraction = 64
	liveTextStepCap  = 64
)

type liveText struct {
	streamingMode output.StreamingMode
	arrivedText   strings.Builder
	drawn         int
	drawnRowCount int
	isTailHidden  bool
}

func (self *liveText) Len() int {
	return self.arrivedText.Len()
}

func (self *liveText) String() string {
	return self.arrivedText.String()
}

func (self *liveText) Text() string {
	return strutil.StripControl(self.arrivedText.String())
}

func (self *liveText) Write(text string) {
	_, _ = self.arrivedText.WriteString(text)
}

func (self *liveText) Reset() {
	self.arrivedText.Reset()
	self.drawn = 0
	self.drawnRowCount = 0
	self.isTailHidden = false
}

func (self *liveText) MarkDrawn() {
	self.drawn = self.arrivedText.Len()
}

func (self *liveText) Take(rows []string, isTailHidden bool) []string {
	if isTailHidden {
		rows = rows[:min(max(len(rows)-1, self.drawnRowCount), len(rows))]
	}

	self.drawnRowCount = len(rows)
	self.isTailHidden = isTailHidden
	self.MarkDrawn()

	return rows
}

func (self *liveText) IsDue() bool {
	if self.streamingMode != output.StreamingModePaced {
		return self.Len() > self.drawn
	}

	return self.Len()-self.drawn >= self.step()
}

func (self *liveText) IsOwed() bool {
	return self.drawn < self.Len() || self.isTailHidden
}

func (self *liveText) step() int {
	return min(max(self.Len()/liveTextFraction, 1), liveTextStepCap)
}
