package painter

import (
	"strings"

	"crdx.org/io/cmd/oh/output"
)

const (
	liveTextFraction = 64
	liveTextStepCap  = 64
)

type liveText struct {
	streamingMode output.StreamingMode
	arrived       strings.Builder
	drawn         int
	isTailHidden  bool
}

func (self *liveText) Len() int {
	return self.arrived.Len()
}

func (self *liveText) String() string {
	return self.arrived.String()
}

func (self *liveText) Write(text string) {
	_, _ = self.arrived.WriteString(text)
}

func (self *liveText) Reset() {
	self.arrived.Reset()
	self.drawn = 0
	self.isTailHidden = false
}

func (self *liveText) MarkDrawn(isTailHidden bool) {
	self.drawn = self.arrived.Len()
	self.isTailHidden = isTailHidden
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
