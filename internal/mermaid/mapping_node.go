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

func (n *node) setCoord(c *drawingCoord) {
	n.drawingCoord = c
}

func (n *node) setDrawing(g *graph) *drawing {
	d := drawBox(n, g)
	n.drawing = d
	return d
}

func (g *graph) setColumnWidth(n *node) {
	col1 := 1
	col2 := 2*g.boxBorderPadding + n.label.width
	col3 := 1
	colsToBePlaced := []int{col1, col2, col3}
	rowsToBePlaced := []int{1, n.label.contentHeight() + 2*g.boxBorderPadding, 1}

	for idx, col := range colsToBePlaced {
		xCoord := n.gridCoord.x + idx
		g.columnWidth[xCoord] = Max(g.columnWidth[xCoord], col)
	}

	for idx, row := range rowsToBePlaced {
		yCoord := n.gridCoord.y + idx
		g.rowHeight[yCoord] = Max(g.rowHeight[yCoord], row)
	}

	if n.gridCoord.x > 0 {
		g.columnWidth[n.gridCoord.x-1] = g.paddingX
	}
	if n.gridCoord.y > 0 {
		basePadding := g.paddingY

		if g.hasIncomingEdgeFromOutsideSubgraph(n) {
			const subgraphOverhead = 4
			basePadding += subgraphOverhead
		}

		g.rowHeight[n.gridCoord.y-1] = Max(g.rowHeight[n.gridCoord.y-1], basePadding)
	}
}

func (g *graph) increaseGridSizeForPath(path []gridCoord) {
	for _, c := range path {
		if _, exists := g.columnWidth[c.x]; !exists {
			g.columnWidth[c.x] = g.paddingX / 2
		}
		if _, exists := g.rowHeight[c.y]; !exists {
			g.rowHeight[c.y] = g.paddingY / 2
		}
	}
}

func (g *graph) reserveSpotInGrid(n *node, requestedCoord *gridCoord) *gridCoord {
	if g.grid[*requestedCoord] != nil {
		if g.graphDirection == "LR" {
			return g.reserveSpotInGrid(n, &gridCoord{x: requestedCoord.x, y: requestedCoord.y + 4})
		} else {
			return g.reserveSpotInGrid(n, &gridCoord{x: requestedCoord.x + 4, y: requestedCoord.y})
		}
	}
	for x := range 3 {
		for y := range 3 {
			reservedCoord := gridCoord{x: requestedCoord.x + x, y: requestedCoord.y + y}
			g.grid[reservedCoord] = n
		}
	}
	n.gridCoord = requestedCoord
	return requestedCoord
}
