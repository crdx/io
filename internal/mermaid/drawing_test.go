package mermaid

import (
	"container/heap"
	"fmt"
	"strings"
	"testing"
)

func TestDrawLineCoversEveryDirectionAndCharacterSet(t *testing.T) {
	for name, test := range map[string]struct {
		from      drawingCoord
		to        drawingCoord
		wantRune  string
		direction direction
	}{
		"up":          {drawingCoord{2, 2}, drawingCoord{2, 0}, "│", Up},
		"down":        {drawingCoord{2, 0}, drawingCoord{2, 2}, "│", Down},
		"left":        {drawingCoord{2, 2}, drawingCoord{0, 2}, "─", Left},
		"right":       {drawingCoord{0, 2}, drawingCoord{2, 2}, "─", Right},
		"upper left":  {drawingCoord{2, 2}, drawingCoord{0, 0}, "╲", UpperLeft},
		"upper right": {drawingCoord{0, 2}, drawingCoord{2, 0}, "╱", UpperRight},
		"lower left":  {drawingCoord{2, 0}, drawingCoord{0, 2}, "╱", LowerLeft},
		"lower right": {drawingCoord{0, 0}, drawingCoord{2, 2}, "╲", LowerRight},
	} {
		for _, useASCII := range []bool{false, true} {
			canvas := mkDrawing(2, 2)
			graph := graph{useAscii: useASCII}
			coordinates := graph.drawLine(canvas, test.from, test.to, 0, 0)
			if len(coordinates) == 0 {
				t.Errorf("%s ascii=%v: drew no coordinates", name, useASCII)
				continue
			}
			wantRune := test.wantRune
			if useASCII {
				switch wantRune {
				case "│":
					wantRune = "|"
				case "─":
					wantRune = "-"
				case "╱":
					wantRune = "/"
				case "╲":
					wantRune = "\\"
				}
			}
			if got := (*canvas)[coordinates[0].x][coordinates[0].y]; got != wantRune {
				t.Errorf("%s ascii=%v: got %q, want %q", name, useASCII, got, wantRune)
			}
			if got := determineDirection(genericCoord(test.from), genericCoord(test.to)); got != test.direction {
				t.Errorf("%s: got direction %v, want %v", name, got, test.direction)
			}
		}
	}
}

func TestEdgeAttachmentDirectionsCoverEveryRelativePosition(t *testing.T) {
	directions := []direction{Up, Down, Left, Right, UpperLeft, UpperRight, LowerLeft, LowerRight, Middle}
	for _, value := range directions {
		if got := value.getOpposite().getOpposite(); got != value {
			t.Errorf("opposite of opposite %v is %v", value, got)
		}
	}

	positions := []gridCoord{
		{0, 0},
		{0, 1},
		{0, 2},
		{1, 0},
		{1, 2},
		{2, 0},
		{2, 1},
		{2, 2},
	}
	for _, graphDirection := range []string{"LR", "TD"} {
		for _, position := range positions {
			from := &node{gridCoord: &gridCoord{1, 1}}
			to := &node{gridCoord: &position}
			graph := graph{graphDirection: graphDirection}
			graph.determineStartAndEndDir(&edge{from: from, to: to})
		}
		self := &node{gridCoord: &gridCoord{1, 1}}
		graph := graph{graphDirection: graphDirection}
		graph.determineStartAndEndDir(&edge{from: self, to: self})
	}
}

func TestDrawRectangleBorderCoversBothCharacterSets(t *testing.T) {
	for _, test := range []struct {
		useASCII bool
		want     string
	}{
		{false, "┌─┐\n│ │\n└─┘"},
		{true, "+-+\n| |\n+-+"},
	} {
		canvas := mkDrawing(2, 2)
		drawRectangleBorder(canvas, drawingCoord{0, 0}, drawingCoord{2, 2}, test.useASCII)
		if got := drawingToString(canvas); got != test.want {
			t.Errorf("ascii=%v: got %q, want %q", test.useASCII, got, test.want)
		}
	}
}

func TestPriorityQueueItemRejectsOtherTypes(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(fmt.Sprint(recovered), "wrong item type") {
			t.Errorf("got panic %v, want wrong item type", recovered)
		}
	}()
	mustPriorityQueueItem("wrong")
}

func TestPriorityQueueOrdersItemsAndRejectsOtherTypes(t *testing.T) {
	queue := &priorityQueue{}
	heap.Init(queue)
	heap.Push(queue, &priorityQueueItem{priority: 20})
	heap.Push(queue, &priorityQueueItem{priority: 10})
	item, ok := heap.Pop(queue).(*priorityQueueItem)
	if !ok {
		t.Fatal("priority queue returned the wrong type")
	}
	if item.priority != 10 {
		t.Errorf("got priority %d, want 10", item.priority)
	}

	defer func() {
		recovered := recover()
		if recovered == nil || !strings.Contains(fmt.Sprint(recovered), "wrong item type") {
			t.Errorf("got panic %v, want wrong item type", recovered)
		}
	}()
	heap.Push(queue, "wrong")
}

func TestDrawingHelpersCoverEmptyAndOverlappingContent(t *testing.T) {
	canvas := mkDrawing(0, 0)
	canvas.drawText(drawingCoord{1, 1}, "資料")
	if !strings.Contains(drawingToString(canvas), "資料") {
		t.Errorf("expected wide text in drawing")
	}

	base := mkDrawing(2, 2)
	(*base)[1][1] = "─"
	overlay := mkDrawing(2, 2)
	(*overlay)[1][1] = "│"
	merged := (&graph{}).mergeDrawings(base, drawingCoord{}, overlay)
	if got := (*merged)[1][1]; got != "┼" {
		t.Errorf("got junction %q, want ┼", got)
	}

	for _, coordinates := range [][2]drawingCoord{
		{{0, 0}, {0, 0}},
		{{0, 1}, {0, 0}},
		{{0, 0}, {0, 1}},
		{{1, 0}, {0, 0}},
		{{0, 0}, {1, 0}},
	} {
		_ = determineDirection(genericCoord(coordinates[0]), genericCoord(coordinates[1]))
	}
}
