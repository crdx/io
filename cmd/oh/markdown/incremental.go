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

	columns                int
	lastCandidate          int
	isDisabled             bool
	shouldRenderHyperlinks bool
	linkRoot               string
}

func (self *IncrementalRenderer) Render(markdown string, columns int) []string {
	return self.render(markdown, columns, false, "")
}

func (self *IncrementalRenderer) RenderWithHyperlinks(markdown string, columns int) []string {
	return self.render(markdown, columns, true, "")
}

func (self *IncrementalRenderer) RenderWithHyperlinksUnder(
	markdown string,
	columns int,
	linkRoot string,
) []string {
	return self.render(markdown, columns, true, linkRoot)
}

func (self *IncrementalRenderer) IsTailMermaid() bool {
	return self.tail.IsTailMermaid()
}

func (self *IncrementalRenderer) Reset() {
	*self = IncrementalRenderer{}
}

func (self *IncrementalRenderer) render(
	markdown string,
	columns int,
	shouldRenderHyperlinks bool,
	linkRoot string,
) []string {
	if columns != self.columns || shouldRenderHyperlinks != self.shouldRenderHyperlinks ||
		linkRoot != self.linkRoot || !strings.HasPrefix(markdown, self.previousSource) {
		self.Reset()
		self.columns = columns
		self.shouldRenderHyperlinks = shouldRenderHyperlinks
		self.linkRoot = linkRoot
	}
	self.previousSource = markdown
	if self.isDisabled {
		return self.tail.render(markdown, columns, shouldRenderHyperlinks, linkRoot)
	}

	tailRows := self.tail.render(markdown[len(self.stableSource):], columns, shouldRenderHyperlinks, linkRoot)
	if self.tail.hasMermaid || self.tail.hasLinkReference {
		return self.disable(markdown, columns)
	}
	if self.tail.hasStableCandidateStart && self.tail.stableCandidateStart > 0 {
		candidate := len(self.stableSource) + self.tail.stableCandidateStart
		if candidate > self.lastCandidate && self.advance(markdown, columns, candidate) {
			tailRows = self.tail.render(
				markdown[len(self.stableSource):],
				columns,
				shouldRenderHyperlinks,
				linkRoot,
			)
		}
	}

	return joinRenderedParts(self.stableRows, tailRows)
}

func (self *IncrementalRenderer) disable(markdown string, columns int) []string {
	self.stableRows = nil
	self.stableSource = ""
	self.tail.Reset()
	self.isDisabled = true

	return self.tail.render(markdown, columns, self.shouldRenderHyperlinks, self.linkRoot)
}

func (self *IncrementalRenderer) advance(markdown string, columns int, candidate int) bool {
	self.lastCandidate = candidate
	stableRows := render(markdown[:candidate], columns, nil, self.shouldRenderHyperlinks, self.linkRoot)
	var tail StreamRenderer
	tailRows := tail.render(markdown[candidate:], columns, self.shouldRenderHyperlinks, self.linkRoot)
	if tail.hasMermaid || tail.hasLinkReference {
		return false
	}
	fullRows := render(markdown, columns, nil, self.shouldRenderHyperlinks, self.linkRoot)
	if !slices.Equal(fullRows, joinRenderedParts(stableRows, tailRows)) {
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
