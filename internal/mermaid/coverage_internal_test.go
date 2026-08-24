package mermaid

import (
	"strings"
	"testing"
)

func TestArrowRoutingAndDrawingEdgeCases(t *testing.T) {
	blocked := &graph{grid: map[gridCoord]*node{}}
	for _, coordinate := range []gridCoord{{1, 0}, {0, 1}} {
		blocked.grid[coordinate] = &node{}
	}
	if _, err := blocked.getPath(gridCoord{}, gridCoord{2, 2}); err == nil {
		t.Fatal("blocked path unexpectedly succeeded")
	}
	open := &graph{grid: map[gridCoord]*node{}}
	if path, err := open.getPath(gridCoord{}, gridCoord{2, 2}); err != nil || len(path) == 0 {
		t.Fatalf("open path: %v, %v", path, err)
	}
	_ = heuristic(gridCoord{}, gridCoord{0, 2})
	_ = heuristic(gridCoord{}, gridCoord{2, 2})

	if got := reverseDrawingLine(nil); got != nil {
		t.Errorf("reverse nil = %#v", got)
	}
	_ = mergePath(nil)
	_ = mergePath([]gridCoord{{0, 0}, {1, 0}, {2, 0}, {2, 1}})

	for _, useASCII := range []bool{false, true} {
		rendererGraph := &graph{
			drawing:     mkDrawing(30, 30),
			columnWidth: map[int]int{0: 1, 1: 2, 2: 2, 3: 2},
			rowHeight:   map[int]int{0: 1, 1: 2, 2: 2, 3: 2},
			useAscii:    useASCII,
		}
		_, _, _, _, _ = rendererGraph.drawArrow(&edge{})
		paths := [][]gridCoord{
			{{1, 1}, {1, 0}},
			{{1, 1}, {1, 2}},
			{{1, 1}, {0, 1}},
			{{1, 1}, {2, 1}},
			{{1, 1}, {2, 0}},
			{{1, 1}, {0, 0}},
			{{1, 1}, {2, 2}},
			{{1, 1}, {0, 2}},
		}
		for _, path := range paths {
			drawing, lines, directions := rendererGraph.drawPath(path)
			_ = drawing
			if len(lines) > 0 {
				_ = rendererGraph.drawBoxStart(path, lines[0])
				_ = rendererGraph.drawArrowHead(lines[0], directions[0])
			}
			_ = rendererGraph.drawCorners(append(path, gridCoord{1, 1}))
		}
		_ = rendererGraph.drawArrowHead(nil, Middle)
		for _, direction := range []direction{Up, Down, Left, Right, UpperRight, UpperLeft, LowerRight, LowerLeft, Middle} {
			_ = rendererGraph.drawArrowHead([]drawingCoord{{3, 3}}, direction)
			_ = rendererGraph.drawArrowHead([]drawingCoord{{3, 3}, {3, 3}}, direction)
		}
		zeroWidth := &graph{drawing: mkDrawing(2, 2), columnWidth: map[int]int{}, rowHeight: map[int]int{}}
		_, _, _ = zeroWidth.drawPath([]gridCoord{{0, 0}, {1, 0}})
		unitWidth := &graph{drawing: mkDrawing(2, 2), columnWidth: map[int]int{0: 1, 1: 1}, rowHeight: map[int]int{0: 1}}
		_, _, _ = unitWidth.drawPath([]gridCoord{{0, 0}, {1, 0}})
	}

	for _, test := range []struct {
		line       []drawingCoord
		start, end int
	}{
		{[]drawingCoord{{0, 0}}, 1, 1},
		{[]drawingCoord{{0, 0}, {3, 0}}, 0, 0},
		{[]drawingCoord{{0, 0}, {3, 0}}, 1, 2},
		{[]drawingCoord{{3, 0}, {0, 0}}, 1, 2},
		{[]drawingCoord{{0, 0}, {0, 3}}, 1, 2},
		{[]drawingCoord{{0, 3}, {0, 0}}, 1, 2},
		{[]drawingCoord{{0, 0}, {3, 3}}, 1, 2},
	} {
		_ = insetLine(test.line, test.start, test.end)
	}
	cornerGraph := &graph{drawing: mkDrawing(10, 10), columnWidth: map[int]int{0: 2, 1: 2, 2: 2}, rowHeight: map[int]int{0: 2, 1: 2, 2: 2}}
	for _, path := range [][]gridCoord{
		{{0, 1}, {1, 1}, {2, 1}},
		{{1, 2}, {1, 1}, {0, 1}},
		{{0, 1}, {1, 1}, {1, 0}},
		{{2, 1}, {1, 1}, {1, 2}},
		{{1, 0}, {1, 1}, {2, 1}},
	} {
		_ = cornerGraph.drawCorners(path)
	}

	canvas := mkDrawing(20, 20)
	for _, line := range [][]drawingCoord{{{5, 5}, {15, 5}}, {{15, 5}, {5, 5}}, {{10, 5}, {10, 15}}, {{10, 15}, {10, 5}}} {
		canvas.drawTextOnLine(line, "label")
	}
}

func TestMappingEdgeAndNodeEdgeCases(t *testing.T) {
	for _, graphDirection := range []string{"LR", "TD"} {
		graph := &graph{
			grid:             map[gridCoord]*node{},
			edgeCounts:       map[edgePair]int{},
			columnWidth:      map[int]int{},
			rowHeight:        map[int]int{},
			graphDirection:   graphDirection,
			paddingX:         5,
			paddingY:         5,
			boxBorderPadding: 1,
		}
		first := &node{name: "A", index: 0, label: newGraphLabel("A")}
		second := &node{name: "B", index: 1, label: newGraphLabel("B")}
		graph.nodes = []*node{first, second}
		graph.reserveSpotInGrid(first, &gridCoord{})
		graph.reserveSpotInGrid(second, &gridCoord{})
		graph.setColumnWidth(first)
		graph.setColumnWidth(second)
		graph.increaseGridSizeForPath([]gridCoord{{20, 20}})

		relationshipEdge := &edge{from: first, to: second, text: strings.Repeat("label", 8), isBidirectional: true}
		for duplicate := range 5 {
			_, _, _ = graph.parallelDirections(relationshipEdge, duplicate)
		}
		graph.determinePath(relationshipEdge)
		if len(relationshipEdge.path) >= 2 {
			graph.determineLabelLine(relationshipEdge)
		}
		graph.determineLabelLine(&edge{path: []gridCoord{{0, 0}, {0, 1}}, text: "x"})
		_ = graph.isNodeColumn(100)
		_ = graph.calculateLineWidth([]gridCoord{{0, 0}, {1, 0}})
	}
	_ = labelMiddleX([]gridCoord{{5, 0}, {1, 0}})

	for _, test := range []struct {
		graphDirection string
		from, to       gridCoord
	}{
		{"LR", gridCoord{0, 0}, gridCoord{4, 0}},
		{"TD", gridCoord{0, 0}, gridCoord{0, 4}},
	} {
		from := &node{gridCoord: &test.from}
		to := &node{gridCoord: &test.to}
		rendererGraph := &graph{graphDirection: test.graphDirection}
		for duplicate := 1; duplicate <= 4; duplicate++ {
			_, _, _ = rendererGraph.parallelDirections(&edge{from: from, to: to}, duplicate)
		}
	}

	labelGraph := &graph{columnWidth: map[int]int{0: 1, 1: 1, 2: 20}, nodes: []*node{{gridCoord: nil}, {gridCoord: &gridCoord{0, 0}}}}
	labelGraph.determineLabelLine(&edge{path: []gridCoord{{0, 0}, {1, 0}, {2, 0}}, text: strings.Repeat("x", 10)})
	_ = labelGraph.isNodeColumn(0)
	wideLineGraph := &graph{columnWidth: map[int]int{0: 20, 1: 20}}
	wideLineGraph.determineLabelLine(&edge{path: []gridCoord{{0, 0}, {1, 0}}, text: "x"})
	shortLineGraph := &graph{columnWidth: map[int]int{0: 1, 1: 1, 2: 1}}
	shortLineGraph.determineLabelLine(&edge{path: []gridCoord{{0, 0}, {1, 0}, {2, 0}}, text: strings.Repeat("x", 20)})
	zeroLineGraph := &graph{columnWidth: map[int]int{}, nodes: []*node{{gridCoord: &gridCoord{0, 0}}}}
	zeroLineGraph.determineLabelLine(&edge{path: []gridCoord{{0, 0}, {1, 0}}, text: "x"})

	fullyBlocked := &graph{grid: map[gridCoord]*node{}, edgeCounts: map[edgePair]int{}, graphDirection: "LR"}
	for x := range 20 {
		for y := range 20 {
			fullyBlocked.grid[gridCoord{x, y}] = &node{}
		}
	}
	blockedFrom, blockedTo := gridCoord{5, 5}, gridCoord{10, 10}
	fullyBlocked.determinePath(&edge{from: &node{index: 0, gridCoord: &blockedFrom}, to: &node{index: 1, gridCoord: &blockedTo}})

	alternativeBlocked := &graph{grid: map[gridCoord]*node{}, edgeCounts: map[edgePair]int{}, graphDirection: "LR"}
	for _, coordinate := range []gridCoord{{8, 6}, {6, 6}, {7, 7}, {7, 5}} {
		alternativeBlocked.grid[coordinate] = &node{}
	}
	alternativeBlocked.determinePath(&edge{from: &node{index: 0, gridCoord: &blockedFrom}, to: &node{index: 1, gridCoord: &blockedTo}})

	preferredBlocked := &graph{grid: map[gridCoord]*node{}, edgeCounts: map[edgePair]int{}, graphDirection: "LR"}
	for _, coordinate := range []gridCoord{{7, 7}, {5, 7}, {6, 8}, {6, 6}} {
		preferredBlocked.grid[coordinate] = &node{}
	}
	preferredBlocked.determinePath(&edge{from: &node{index: 0, gridCoord: &blockedFrom}, to: &node{index: 1, gridCoord: &blockedTo}})
}

func TestDirectionEdgeCases(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("unknown direction did not panic")
			}
		}()
		_ = direction{99, 99}.getOpposite()
	}()

	from := &node{gridCoord: &gridCoord{1, 1}}
	for _, position := range []gridCoord{{1, 0}, {0, 0}, {2, 0}} {
		to := &node{gridCoord: &position}
		graph := graph{graphDirection: "unknown"}
		graph.determineStartAndEndDir(&edge{from: from, to: to})
	}
}

func TestGraphSubgraphEdgeCases(t *testing.T) {
	if _, err := Render("graph LR\nX\nsubgraph group\nA --> B\nend"); err != nil {
		t.Fatalf("separated subgraph roots: %v", err)
	}

	external := &node{name: "X", gridCoord: &gridCoord{0, 0}}
	top := &node{name: "A", gridCoord: &gridCoord{0, 1}}
	bottom := &node{name: "B", gridCoord: &gridCoord{0, 2}}
	missing := &node{name: "missing"}
	nodeSubgraph := &subgraph{nodes: []*node{top, bottom, missing}}
	rendererGraph := &graph{subgraphs: []*subgraph{nodeSubgraph}, edges: []*edge{{from: external, to: top}, {from: external, to: bottom}}}
	if rendererGraph.hasIncomingEdgeFromOutsideSubgraph(external) {
		t.Error("external node reported an incoming subgraph edge")
	}
	if rendererGraph.hasIncomingEdgeFromOutsideSubgraph(missing) {
		t.Error("node without an external edge reported one")
	}
	if !rendererGraph.hasIncomingEdgeFromOutsideSubgraph(top) {
		t.Error("top external target was not recognised")
	}
	if rendererGraph.hasIncomingEdgeFromOutsideSubgraph(bottom) {
		t.Error("lower external target was preferred")
	}
	rendererGraph.columnWidth = map[int]int{}
	rendererGraph.rowHeight = map[int]int{}
	rendererGraph.paddingY = 5
	top.label = newGraphLabel("A")
	rendererGraph.setColumnWidth(top)

	for _, subgraphs := range [][]*subgraph{
		{{nodes: []*node{{}}, minX: 0, maxX: 10, minY: 5, maxY: 10}, {nodes: []*node{{}}, minX: 2, maxX: 8, minY: 0, maxY: 8}},
		{{nodes: []*node{{}}, minX: 5, maxX: 10, minY: 0, maxY: 10}, {nodes: []*node{{}}, minX: 0, maxX: 8, minY: 2, maxY: 8}},
		{{nodes: []*node{{}}, minX: 0, maxX: 5, minY: 0, maxY: 10}, {nodes: []*node{{}}, minX: 5, maxX: 10, minY: 2, maxY: 8}},
		{{nodes: []*node{{}}, minX: 5, maxX: 10, minY: 0, maxY: 10}, {nodes: []*node{{}}, minX: 0, maxX: 5, minY: 2, maxY: 8}},
	} {
		(&graph{subgraphs: subgraphs}).ensureSubgraphSpacing()
	}

	boxGraph := &graph{}
	boxGraph.calculateSubgraphBoundingBox(&subgraph{nodes: []*node{{}}})
	boxGraph.subgraphs = []*subgraph{{minX: 0, minY: 0}}
	boxGraph.offsetDrawingForSubgraphs()
	boxGraph.drawing = mkDrawing(1, 1)
	boxGraph.drawSubgraphLabels()
}

func TestSubgraphAndStyleDrawingEdgeCases(t *testing.T) {
	for _, useASCII := range []bool{false, true} {
		graph := graph{useAscii: useASCII}
		_ = drawSubgraph(&subgraph{}, graph)
		_ = drawSubgraph(&subgraph{minX: 0, minY: 0, maxX: 4, maxY: 4}, graph)
		_, _ = drawSubgraphLabel(&subgraph{})
		label := newGraphLabel("wide 資料\nsecond")
		_, _ = drawSubgraphLabel(&subgraph{label: label, minX: 0, minY: 0, maxX: 7, maxY: 8})
		_, _ = drawSubgraphLabel(&subgraph{label: newGraphLabel("資料"), minX: 0, minY: 0, maxX: 3, maxY: 8})
	}
	for _, styleType := range []string{"html", "cli", "unknown"} {
		_ = wrapTextInColor("x", "#ffffff", styleType)
	}
	_ = wrapTextInColor("x", "", "cli")

	parent := &subgraph{name: "parent", nodes: []*node{{}}}
	child := &subgraph{name: "child", parent: parent, nodes: []*node{{}}, minX: -2, minY: -3, maxX: 3, maxY: 3}
	parent.children = []*subgraph{child}
	graph := &graph{subgraphs: []*subgraph{child, parent}, drawing: mkDrawing(10, 10)}
	_ = graph.sortSubgraphsByDepth()
	graph.offsetDrawingForSubgraphs()
	graph.drawSubgraphLabels()
	graph.calculateSubgraphBoundingBox(&subgraph{})

	a := &subgraph{nodes: []*node{{}}, minX: 0, minY: 0, maxX: 10, maxY: 10}
	b := &subgraph{nodes: []*node{{}}, minX: 2, minY: 5, maxX: 8, maxY: 12}
	c := &subgraph{nodes: []*node{{}}, minX: 5, minY: 2, maxX: 12, maxY: 8}
	graph.subgraphs = []*subgraph{a, b, c}
	graph.ensureSubgraphSpacing()
}
