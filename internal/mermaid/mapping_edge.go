package mermaid

import "crdx.org/io/internal/mermaid/runewidth"

type edge struct {
	from            *node
	to              *node
	text            string
	isBidirectional bool
	path            []gridCoord
	labelLine       []gridCoord
	startDir        direction
	endDir          direction
}

func (self *graph) determinePath(e *edge) {
	key := newEdgePair(e.from.index, e.to.index)
	duplicateIndex := self.edgeCounts[key]

	if startDir, endDir, ok := self.parallelDirections(e, duplicateIndex); ok {
		from := e.from.gridCoord.Direction(startDir)
		to := e.to.gridCoord.Direction(endDir)
		if path, err := self.getPath(from, to); err == nil {
			e.startDir = startDir
			e.endDir = endDir
			e.path = mergePath(path)
			self.edgeCounts[key]++
			return
		}
	}

	preferredDir, preferredOppositeDir, alternativeDir, alternativeOppositeDir := self.determineStartAndEndDir(e)
	preferredFrom := e.from.gridCoord.Direction(preferredDir)
	preferredTo := e.to.gridCoord.Direction(preferredOppositeDir)
	preferredPath, preferredError := self.getPath(preferredFrom, preferredTo)
	if preferredError == nil {
		preferredPath = mergePath(preferredPath)
	}

	alternativeFrom := e.from.gridCoord.Direction(alternativeDir)
	alternativeTo := e.to.gridCoord.Direction(alternativeOppositeDir)
	alternativePath, alternativeError := self.getPath(alternativeFrom, alternativeTo)
	if alternativeError == nil {
		alternativePath = mergePath(alternativePath)
	}

	switch {
	case preferredError == nil && (alternativeError != nil || len(preferredPath) <= len(alternativePath)):
		e.startDir = preferredDir
		e.endDir = preferredOppositeDir
		e.path = preferredPath
	case alternativeError == nil:
		e.startDir = alternativeDir
		e.endDir = alternativeOppositeDir
		e.path = alternativePath
	default:
		return
	}
	self.edgeCounts[key]++
}

func (self *graph) parallelDirections(e *edge, duplicateIndex int) (direction, direction, bool) {
	if duplicateIndex == 0 {
		return Middle, Middle, false
	}

	dir := determineDirection(genericCoord(*e.from.gridCoord), genericCoord(*e.to.gridCoord))
	switch {
	case self.graphDirection == "LR" && (dir == Right || dir == Left):
		options := [][2]direction{{Down, Down}, {Up, Up}}
		if duplicateIndex-1 < len(options) {
			return options[duplicateIndex-1][0], options[duplicateIndex-1][1], true
		}
	case self.graphDirection == "TD" && (dir == Down || dir == Up):
		options := [][2]direction{{Right, Right}, {Left, Left}}
		if duplicateIndex-1 < len(options) {
			return options[duplicateIndex-1][0], options[duplicateIndex-1][1], true
		}
	}

	return Middle, Middle, false
}

func (self *graph) determineLabelLine(e *edge) {
	lenLabel := runewidth.StringWidth(e.text)
	if lenLabel == 0 {
		return
	}
	prevStep := e.path[0]
	var largestLine []gridCoord
	var largestLineSize int
	var fallbackLine []gridCoord
	var fallbackLineSize int
	for _, step := range e.path[1:] {
		line := []gridCoord{prevStep, step}
		prevStep = step
		lineWidth := self.calculateLineWidth(line)
		if self.isNodeColumn(labelMiddleX(line)) {
			if lineWidth > fallbackLineSize {
				fallbackLineSize = lineWidth
				fallbackLine = line
			}
			continue
		}
		if lineWidth >= lenLabel {
			largestLine = line
			break
		}
		if lineWidth > largestLineSize {
			largestLineSize = lineWidth
			largestLine = line
		}
	}
	if largestLine == nil {
		largestLine = fallbackLine
	}
	if largestLine == nil {
		largestLine = []gridCoord{e.path[0], e.path[1]}
	}

	middleX := labelMiddleX(largestLine)
	labelPadding := 3
	if e.isBidirectional {
		labelPadding = 4
	}
	self.columnWidth[middleX] = Max(self.columnWidth[middleX], lenLabel+labelPadding)
	e.labelLine = largestLine
}

func labelMiddleX(line []gridCoord) int {
	minX, maxX := line[0].x, line[1].x
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	return minX + (maxX-minX)/2
}

func (self *graph) isNodeColumn(x int) bool {
	for _, n := range self.nodes {
		if n.gridCoord == nil {
			continue
		}
		if x >= n.gridCoord.x && x <= n.gridCoord.x+2 {
			return true
		}
	}
	return false
}

func (self *graph) calculateLineWidth(line []gridCoord) int {
	totalSize := 0
	for _, c := range line {
		totalSize += self.columnWidth[c.x]
	}
	return totalSize
}
