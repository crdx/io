package markdown

import (
	"slices"
	"strings"
)

type IncrementalRenderer struct {
	stableRows     []string
	stableSource   string
	previousSource string
	tail           StreamRenderer

	columns       int
	lastCandidate int
	isDisabled    bool
}

func (self *IncrementalRenderer) Render(markdown string, columns int) []string {
	if columns != self.columns || !strings.HasPrefix(markdown, self.previousSource) {
		self.Reset()
		self.columns = columns
	}
	self.previousSource = markdown
	if self.isDisabled {
		return self.tail.Render(markdown, columns)
	}

	tailRows := self.tail.Render(markdown[len(self.stableSource):], columns)
	if self.tail.hasMermaid || self.tail.hasLinkReference {
		return self.disable(markdown, columns)
	}
	if self.tail.hasStableCandidateStart && self.tail.stableCandidateStart > 0 {
		candidate := len(self.stableSource) + self.tail.stableCandidateStart
		if candidate > self.lastCandidate && self.advance(markdown, columns, candidate) {
			tailRows = self.tail.Render(markdown[len(self.stableSource):], columns)
		}
	}

	return joinRenderedParts(self.stableRows, tailRows)
}

func (self *IncrementalRenderer) IsTailMermaid() bool {
	return self.tail.IsTailMermaid()
}

func (self *IncrementalRenderer) Reset() {
	*self = IncrementalRenderer{}
}

func (self *IncrementalRenderer) disable(markdown string, columns int) []string {
	self.stableRows = nil
	self.stableSource = ""
	self.tail.Reset()
	self.isDisabled = true

	return self.tail.Render(markdown, columns)
}

func (self *IncrementalRenderer) advance(markdown string, columns int, candidate int) bool {
	self.lastCandidate = candidate
	stableRows := Render(markdown[:candidate], columns)
	var tail StreamRenderer
	tailRows := tail.Render(markdown[candidate:], columns)
	if tail.hasMermaid || tail.hasLinkReference {
		return false
	}
	if fullRows := Render(markdown, columns); !slices.Equal(fullRows, joinRenderedParts(stableRows, tailRows)) {
		return false
	}

	self.stableRows = stableRows
	self.stableSource = markdown[:candidate]
	self.tail.Reset()
	return true
}

func joinRenderedParts(stableRows []string, tailRows []string) []string {
	if len(stableRows) == 0 {
		return slices.Clone(tailRows)
	}
	if len(tailRows) == 0 {
		return slices.Clone(stableRows)
	}

	rows := make([]string, 0, len(stableRows)+1+len(tailRows))
	rows = append(rows, stableRows...)
	rows = append(rows, "")
	return append(rows, tailRows...)
}
