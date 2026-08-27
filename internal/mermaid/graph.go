package mermaid

import (
	"errors"
	"slices"

	"crdx.org/io/internal/mermaid/orderedmap"
)

type genericCoord struct {
	x int
	y int
}

type (
	gridCoord    genericCoord
	drawingCoord genericCoord
)

func (c gridCoord) Equals(other gridCoord) bool {
	return c.x == other.x && c.y == other.y
}

func (c drawingCoord) Equals(other drawingCoord) bool {
	return c.x == other.x && c.y == other.y
}

func (g *graph) lineToDrawing(line []gridCoord) []drawingCoord {
	dc := []drawingCoord{}
	for _, c := range line {
		dc = append(dc, g.gridToDrawingCoord(c))
	}
	return dc
}

type graph struct {
	nodes            []*node
	edges            []*edge
	drawing          *drawing
	grid             map[gridCoord]*node
	edgeCounts       map[edgePair]int
	columnWidth      map[int]int
	rowHeight        map[int]int
	styleClasses     map[string]styleClass
	styleType        string
	boxBorderPadding int
	graphDirection   string
	paddingX         int
	paddingY         int
	subgraphs        []*subgraph
	offsetX          int
	offsetY          int
	useAscii         bool
}

type edgePair struct {
	from int
	to   int
}

func newEdgePair(from, to int) edgePair {
	if from < to {
		return edgePair{from: from, to: to}
	}
	return edgePair{from: to, to: from}
}

type subgraph struct {
	name     string
	label    graphLabel
	nodes    []*node
	parent   *subgraph
	children []*subgraph
	minX     int
	minY     int
	maxX     int
	maxY     int
}

func mkGraph(data *orderedmap.OrderedMap[string, []textEdge], nodeSpecs map[string]graphNodeSpec) graph {
	g := graph{drawing: mkDrawing(0, 0)}
	g.grid = make(map[gridCoord]*node)
	g.edgeCounts = make(map[edgePair]int)
	g.columnWidth = make(map[int]int)
	g.rowHeight = make(map[int]int)
	g.styleClasses = make(map[string]styleClass)
	index := 0
	for el := data.Front(); el != nil; el = el.Next() {
		nodeName := el.Key
		children := el.Value
		spec := nodeSpecs[nodeName]
		parentNode, err := g.getNode(nodeName)
		if err != nil {
			parentNode = &node{name: nodeName, label: spec.label, index: index, styleClassName: spec.styleClass}
			g.appendNode(parentNode)
			index += 1
		}
		for _, textEdge := range children {
			childSpec := nodeSpecs[textEdge.child.name]
			childNode, err := g.getNode(textEdge.child.name)
			if err != nil {
				childNode = &node{name: textEdge.child.name, label: childSpec.label, index: index, styleClassName: childSpec.styleClass}
				g.appendNode(childNode)
				index += 1
			}
			e := edge{
				from:            parentNode,
				to:              childNode,
				text:            textEdge.label,
				isBidirectional: textEdge.isBidirectional,
			}
			g.edges = append(g.edges, &e)
		}
	}
	return g
}

func (g *graph) setStyleClasses(properties *graphProperties) {
	g.styleClasses = *properties.styleClasses
	g.styleType = properties.styleType
	g.boxBorderPadding = properties.boxBorderPadding
	g.graphDirection = properties.graphDirection
	g.paddingX = properties.paddingX
	g.paddingY = properties.paddingY
	for _, n := range g.nodes {
		if n.styleClassName != "" {
			n.styleClass = g.styleClasses[n.styleClassName]
		}
	}
}

func (g *graph) setSubgraphs(textSubgraphs []*textSubgraph) {
	g.subgraphs = []*subgraph{}

	for _, tsg := range textSubgraphs {
		sg := &subgraph{
			name:     tsg.name,
			label:    tsg.label,
			nodes:    []*node{},
			children: []*subgraph{},
		}

		for _, nodeName := range tsg.nodes {
			node, err := g.getNode(nodeName)
			if err == nil {
				sg.nodes = append(sg.nodes, node)
			}
		}

		g.subgraphs = append(g.subgraphs, sg)
	}

	for i, tsg := range textSubgraphs {
		sg := g.subgraphs[i]

		if tsg.parent != nil {
			for j, parentTsg := range textSubgraphs {
				if parentTsg == tsg.parent {
					sg.parent = g.subgraphs[j]
					break
				}
			}
		}

		for _, childTsg := range tsg.children {
			for j, checkTsg := range textSubgraphs {
				if checkTsg == childTsg {
					sg.children = append(sg.children, g.subgraphs[j])
					break
				}
			}
		}
	}
}

func (g *graph) createMapping() error {
	highestPositionPerLevel := map[int]int{}

	nodesFound := make(map[string]bool)
	rootNodes := []*node{}
	for _, n := range g.nodes {
		if _, ok := nodesFound[n.name]; !ok {
			rootNodes = append(rootNodes, n)
		}
		nodesFound[n.name] = true
		for _, child := range g.getChildren(n) {
			nodesFound[child.name] = true
		}
	}

	hasExternalRoots := false
	hasSubgraphRootsWithEdges := false
	for _, n := range rootNodes {
		if g.isNodeInAnySubgraph(n) {
			if len(g.getChildren(n)) > 0 {
				hasSubgraphRootsWithEdges = true
			}
		} else {
			hasExternalRoots = true
		}
	}

	shouldSeparate := g.graphDirection == "LR" && hasExternalRoots && hasSubgraphRootsWithEdges

	externalRootNodes := []*node{}
	subgraphRootNodes := []*node{}
	if shouldSeparate {
		for _, n := range rootNodes {
			if g.isNodeInAnySubgraph(n) {
				subgraphRootNodes = append(subgraphRootNodes, n)
			} else {
				externalRootNodes = append(externalRootNodes, n)
			}
		}
	} else {
		externalRootNodes = rootNodes
	}

	for _, n := range externalRootNodes {
		var mappingCoord *gridCoord
		if g.graphDirection == "LR" {
			mappingCoord = g.reserveSpotInGrid(g.nodes[n.index], &gridCoord{x: 0, y: highestPositionPerLevel[0]})
		} else {
			mappingCoord = g.reserveSpotInGrid(g.nodes[n.index], &gridCoord{x: highestPositionPerLevel[0], y: 0})
		}
		g.nodes[n.index].gridCoord = mappingCoord
		highestPositionPerLevel[0] += 4
	}

	if shouldSeparate && len(subgraphRootNodes) > 0 {
		subgraphLevel := 4
		for _, n := range subgraphRootNodes {
			mappingCoord := g.reserveSpotInGrid(g.nodes[n.index], &gridCoord{x: subgraphLevel, y: highestPositionPerLevel[subgraphLevel]})
			g.nodes[n.index].gridCoord = mappingCoord
			highestPositionPerLevel[subgraphLevel] += 4
		}
	}

	for _, n := range g.nodes {
		var childLevel int
		if g.graphDirection == "LR" {
			childLevel = n.gridCoord.x + 4
		} else {
			childLevel = n.gridCoord.y + 4
		}
		highestPosition := highestPositionPerLevel[childLevel]
		for _, child := range g.getChildren(n) {
			if child.gridCoord != nil {
				continue
			}

			var mappingCoord *gridCoord
			if g.graphDirection == "LR" {
				mappingCoord = g.reserveSpotInGrid(g.nodes[child.index], &gridCoord{x: childLevel, y: highestPosition})
			} else {
				mappingCoord = g.reserveSpotInGrid(g.nodes[child.index], &gridCoord{x: highestPosition, y: childLevel})
			}
			g.nodes[child.index].gridCoord = mappingCoord
			highestPositionPerLevel[childLevel] = highestPosition + 4
		}
	}

	for _, n := range g.nodes {
		g.setColumnWidth(n)
	}

	for _, e := range g.edges {
		g.determinePath(e)
		g.increaseGridSizeForPath(e.path)
		g.determineLabelLine(e)
	}

	for _, n := range g.nodes {
		dc := g.gridToDrawingCoord(*n.gridCoord)
		g.nodes[n.index].setCoord(&dc)
		g.nodes[n.index].setDrawing(g)
	}

	g.calculateSubgraphBoundingBoxes()

	g.offsetDrawingForSubgraphs()
	if err := g.validateDrawingSize(); err != nil {
		return err
	}
	g.setDrawingSizeToGridConstraints()
	return nil
}

func (g *graph) validateDrawingSize() error {
	columns := g.offsetX
	rows := g.offsetY
	for _, width := range g.columnWidth {
		columns += width
	}
	for _, height := range g.rowHeight {
		rows += height
	}
	for _, subgraph := range g.subgraphs {
		columns = max(columns, subgraph.maxX+1)
		rows = max(rows, subgraph.maxY+1)
	}
	return validateCanvasLimits(columns, rows)
}

func (g *graph) calculateSubgraphBoundingBoxes() {
	for _, sg := range g.subgraphs {
		g.calculateSubgraphBoundingBox(sg)
	}

	g.ensureSubgraphSpacing()
}

func (g *graph) isNodeInAnySubgraph(n *node) bool {
	for _, sg := range g.subgraphs {
		if slices.Contains(sg.nodes, n) {
			return true
		}
	}
	return false
}

func (g *graph) getNodeSubgraph(n *node) *subgraph {
	for _, sg := range g.subgraphs {
		if slices.Contains(sg.nodes, n) {
			return sg
		}
	}
	return nil
}

func (g *graph) hasIncomingEdgeFromOutsideSubgraph(n *node) bool {
	nodeSubgraph := g.getNodeSubgraph(n)
	if nodeSubgraph == nil {
		return false
	}

	hasExternalEdge := false
	for _, edge := range g.edges {
		if edge.to == n {
			sourceSubgraph := g.getNodeSubgraph(edge.from)
			if sourceSubgraph != nodeSubgraph {
				hasExternalEdge = true
				break
			}
		}
	}

	if !hasExternalEdge {
		return false
	}

	for _, otherNode := range nodeSubgraph.nodes {
		if otherNode == n || otherNode.gridCoord == nil {
			continue
		}
		otherHasExternal := false
		for _, edge := range g.edges {
			if edge.to == otherNode {
				sourceSubgraph := g.getNodeSubgraph(edge.from)
				if sourceSubgraph != nodeSubgraph {
					otherHasExternal = true
					break
				}
			}
		}
		if otherHasExternal && otherNode.gridCoord.y < n.gridCoord.y {
			return false
		}
	}

	return true
}

func (g *graph) ensureSubgraphSpacing() {
	const minSpacing = 1

	rootSubgraphs := []*subgraph{}
	for _, sg := range g.subgraphs {
		if sg.parent == nil && len(sg.nodes) > 0 {
			rootSubgraphs = append(rootSubgraphs, sg)
		}
	}

	for i := range rootSubgraphs {
		for j := i + 1; j < len(rootSubgraphs); j++ {
			sg1 := rootSubgraphs[i]
			sg2 := rootSubgraphs[j]

			if sg1.minX < sg2.maxX && sg1.maxX > sg2.minX {
				if sg1.maxY >= sg2.minY-minSpacing && sg1.minY < sg2.minY {
					newMinY := sg1.maxY + minSpacing + 1
					sg2.minY = newMinY
				} else if sg2.maxY >= sg1.minY-minSpacing && sg2.minY < sg1.minY {
					newMinY := sg2.maxY + minSpacing + 1
					sg1.minY = newMinY
				}
			}

			if sg1.minY < sg2.maxY && sg1.maxY > sg2.minY {
				if sg1.maxX >= sg2.minX-minSpacing && sg1.minX < sg2.minX {
					newMinX := sg1.maxX + minSpacing + 1
					sg2.minX = newMinX
				} else if sg2.maxX >= sg1.minX-minSpacing && sg2.minX < sg1.minX {
					newMinX := sg2.maxX + minSpacing + 1
					sg1.minX = newMinX
				}
			}
		}
	}
}

func (g *graph) calculateSubgraphBoundingBox(sg *subgraph) {
	if len(sg.nodes) == 0 {
		return
	}

	minX := 1000000
	minY := 1000000
	maxX := -1000000
	maxY := -1000000

	for _, child := range sg.children {
		g.calculateSubgraphBoundingBox(child)
		if len(child.nodes) > 0 {
			minX = Min(minX, child.minX)
			minY = Min(minY, child.minY)
			maxX = Max(maxX, child.maxX)
			maxY = Max(maxY, child.maxY)
		}
	}

	for _, node := range sg.nodes {
		if node.drawingCoord == nil || node.drawing == nil {
			continue
		}

		nodeMinX := node.drawingCoord.x
		nodeMinY := node.drawingCoord.y
		nodeMaxX := nodeMinX + len(*node.drawing) - 1
		nodeMaxY := nodeMinY + len((*node.drawing)[0]) - 1

		minX = Min(minX, nodeMinX)
		minY = Min(minY, nodeMinY)
		maxX = Max(maxX, nodeMaxX)
		maxY = Max(maxY, nodeMaxY)
	}

	currentWidth := maxX - minX
	currentInnerWidth := currentWidth + 3
	if currentInnerWidth < sg.label.width {
		extraWidth := sg.label.width - currentInnerWidth
		minX -= extraWidth / 2
		maxX += extraWidth - (extraWidth / 2)
	}

	const subgraphPadding = 2
	subgraphLabelSpace := sg.label.contentHeight() + 1
	sg.minX = minX - subgraphPadding
	sg.minY = minY - subgraphPadding - subgraphLabelSpace
	sg.maxX = maxX + subgraphPadding
	sg.maxY = maxY + subgraphPadding
}

func (g *graph) offsetDrawingForSubgraphs() {
	if len(g.subgraphs) == 0 {
		return
	}

	minX := 0
	minY := 0
	for _, sg := range g.subgraphs {
		minX = Min(minX, sg.minX)
		minY = Min(minY, sg.minY)
	}

	offsetX := -minX
	offsetY := -minY

	if offsetX == 0 && offsetY == 0 {
		return
	}

	g.offsetX = offsetX
	g.offsetY = offsetY

	for _, sg := range g.subgraphs {
		sg.minX += offsetX
		sg.minY += offsetY
		sg.maxX += offsetX
		sg.maxY += offsetY
	}

	for _, n := range g.nodes {
		if n.drawingCoord != nil {
			n.drawingCoord.x += offsetX
			n.drawingCoord.y += offsetY
		}
	}
}

func (g *graph) draw() *drawing {
	g.drawSubgraphs()

	for _, node := range g.nodes {
		if !node.drawn {
			g.drawNode(node)
		}
	}
	lineDrawings := []*drawing{}
	cornerDrawings := []*drawing{}
	arrowHeadDrawings := []*drawing{}
	boxStartDrawings := []*drawing{}
	labelDrawings := []*drawing{}
	for _, edge := range g.edges {
		line, boxStart, arrowHead, corners, label := g.drawEdge(edge)
		lineDrawings = append(lineDrawings, line)
		cornerDrawings = append(cornerDrawings, corners)
		arrowHeadDrawings = append(arrowHeadDrawings, arrowHead)
		boxStartDrawings = append(boxStartDrawings, boxStart)
		labelDrawings = append(labelDrawings, label)
	}

	g.drawing = g.mergeDrawings(g.drawing, drawingCoord{0, 0}, lineDrawings...)
	g.drawing = g.mergeDrawings(g.drawing, drawingCoord{0, 0}, cornerDrawings...)
	g.drawing = g.mergeDrawings(g.drawing, drawingCoord{0, 0}, arrowHeadDrawings...)
	g.drawing = g.mergeDrawings(g.drawing, drawingCoord{0, 0}, boxStartDrawings...)
	g.drawing = g.mergeDrawings(g.drawing, drawingCoord{0, 0}, labelDrawings...)

	g.drawSubgraphLabels()

	return g.drawing
}

func (g *graph) drawSubgraphs() {
	sorted := g.sortSubgraphsByDepth()

	for _, sg := range sorted {
		sgDrawing := drawSubgraph(sg, *g)
		offset := drawingCoord{sg.minX, sg.minY}
		g.drawing = g.mergeDrawings(g.drawing, offset, sgDrawing)
	}
}

func (g *graph) drawSubgraphLabels() {
	for _, sg := range g.subgraphs {
		if len(sg.nodes) == 0 {
			continue
		}
		labelDrawing, offset := drawSubgraphLabel(sg)
		g.drawing = g.mergeDrawings(g.drawing, offset, labelDrawing)
	}
}

func (g *graph) sortSubgraphsByDepth() []*subgraph {
	depths := make(map[*subgraph]int)
	for _, sg := range g.subgraphs {
		depths[sg] = g.getSubgraphDepth(sg)
	}

	sorted := make([]*subgraph, len(g.subgraphs))
	copy(sorted, g.subgraphs)

	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if depths[sorted[i]] > depths[sorted[j]] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

func (g *graph) getSubgraphDepth(sg *subgraph) int {
	if sg.parent == nil {
		return 0
	}
	return 1 + g.getSubgraphDepth(sg.parent)
}

func (g *graph) getNode(nodeName string) (*node, error) {
	for _, n := range g.nodes {
		if n.name == nodeName {
			return n, nil
		}
	}
	return &node{}, errors.New("node " + nodeName + " not found")
}

func (g *graph) appendNode(n *node) {
	g.nodes = append(g.nodes, n)
}

func (g *graph) getEdgesFromNode(n *node) []edge {
	edges := []edge{}
	for _, edge := range g.edges {
		if (edge.from.name) == (n.name) {
			edges = append(edges, *edge)
		}
	}
	return edges
}

func (g *graph) getChildren(n *node) []*node {
	edges := g.getEdgesFromNode(n)
	children := []*node{}
	for _, edge := range edges {
		if edge.from.name == n.name {
			children = append(children, edge.to)
		}
	}
	return children
}

func (g *graph) gridToDrawingCoord(coordinate gridCoord) drawingCoord {
	x := 0
	y := 0
	for column := range coordinate.x {
		x += g.columnWidth[column]
	}
	for row := range coordinate.y {
		y += g.rowHeight[row]
	}
	return drawingCoord{
		x: x + g.columnWidth[coordinate.x]/2 + g.offsetX,
		y: y + g.rowHeight[coordinate.y]/2 + g.offsetY,
	}
}
