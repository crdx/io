package mermaid

type node struct {
	name           string
	label          graphLabel
	drawing        *drawing
	drawingCoord   *drawingCoord
	gridCoord      *gridCoord
	drawn          bool
	index          int // Index of the node in the graph.nodes slice
	styleClassName string
	styleClass     styleClass
}

func (self *node) setCoord(c *drawingCoord) {
	self.drawingCoord = c
}

func (self *node) setDrawing(g *graph) *drawing {
	d := drawBox(self, g)
	self.drawing = d
	return d
}

func (self *graph) setColumnWidth(n *node) {
	col1 := 1
	col2 := 2*self.boxBorderPadding + n.label.width
	col3 := 1
	colsToBePlaced := []int{col1, col2, col3}
	rowsToBePlaced := []int{1, n.label.contentHeight() + 2*self.boxBorderPadding, 1}

	for idx, col := range colsToBePlaced {
		xCoord := n.gridCoord.x + idx
		self.columnWidth[xCoord] = Max(self.columnWidth[xCoord], col)
	}

	for idx, row := range rowsToBePlaced {
		yCoord := n.gridCoord.y + idx
		self.rowHeight[yCoord] = Max(self.rowHeight[yCoord], row)
	}

	if n.gridCoord.x > 0 {
		self.columnWidth[n.gridCoord.x-1] = self.paddingX
	}
	if n.gridCoord.y > 0 {
		basePadding := self.paddingY

		if self.hasIncomingEdgeFromOutsideSubgraph(n) {
			const subgraphOverhead = 4
			basePadding += subgraphOverhead
		}

		self.rowHeight[n.gridCoord.y-1] = Max(self.rowHeight[n.gridCoord.y-1], basePadding)
	}
}

func (self *graph) increaseGridSizeForPath(path []gridCoord) {
	for _, c := range path {
		if _, exists := self.columnWidth[c.x]; !exists {
			self.columnWidth[c.x] = self.paddingX / 2
		}
		if _, exists := self.rowHeight[c.y]; !exists {
			self.rowHeight[c.y] = self.paddingY / 2
		}
	}
}

func (self *graph) reserveSpotInGrid(n *node, requestedCoord *gridCoord) *gridCoord {
	if self.grid[*requestedCoord] != nil {
		if self.graphDirection == "LR" {
			return self.reserveSpotInGrid(n, &gridCoord{x: requestedCoord.x, y: requestedCoord.y + 4})
		} else {
			return self.reserveSpotInGrid(n, &gridCoord{x: requestedCoord.x + 4, y: requestedCoord.y})
		}
	}
	for x := range 3 {
		for y := range 3 {
			reservedCoord := gridCoord{x: requestedCoord.x + x, y: requestedCoord.y + y}
			self.grid[reservedCoord] = n
		}
	}
	n.gridCoord = requestedCoord
	return requestedCoord
}
