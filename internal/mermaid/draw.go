package mermaid

import (
	"fmt"
	"slices"
	"strings"

	"crdx.org/io/internal/mermaid/color"
	"crdx.org/io/internal/mermaid/runewidth"
)

var junctionChars = []string{
	"─",
	"│",
	"┌",
	"┐",
	"└",
	"┘",
	"├",
	"┤",
	"┬",
	"┴",
	"┼",
	"╴",
	"╵",
	"╶",
	"╷",
}

type drawing [][]string

type styleClass struct {
	name   string
	styles map[string]string
}

func (g *graph) drawNode(n *node) {
	m := g.mergeDrawings(g.drawing, *n.drawingCoord, n.drawing)
	g.drawing = m
}

func (g *graph) drawEdge(e *edge) (*drawing, *drawing, *drawing, *drawing, *drawing) {
	return g.drawArrow(e)
}

func (d *drawing) drawText(start drawingCoord, text string) {
	cells := runewidth.Cells(text)
	d.increaseSize(start.x+len(cells), start.y)
	for i, cell := range cells {
		(*d)[start.x+i][start.y] = cell
	}
}

func (g *graph) drawLine(d *drawing, from drawingCoord, to drawingCoord, offsetFrom int, offsetTo int) []drawingCoord {
	direction := determineDirection(genericCoord(from), genericCoord(to))
	vertical, horizontal, rising, falling := "│", "─", "╱", "╲"
	if g.useAscii {
		vertical, horizontal, rising, falling = "|", "-", "/", "\\"
	}

	drawnCoords := make([]drawingCoord, 0)
	switch direction {
	case Up:
		for y := from.y - offsetFrom; y >= to.y-offsetTo; y-- {
			drawnCoords = append(drawnCoords, drawingCoord{from.x, y})
			(*d)[from.x][y] = vertical
		}
	case Down:
		for y := from.y + offsetFrom; y <= to.y+offsetTo; y++ {
			drawnCoords = append(drawnCoords, drawingCoord{from.x, y})
			(*d)[from.x][y] = vertical
		}
	case Left:
		for x := from.x - offsetFrom; x >= to.x-offsetTo; x-- {
			drawnCoords = append(drawnCoords, drawingCoord{x, from.y})
			(*d)[x][from.y] = horizontal
		}
	case Right:
		for x := from.x + offsetFrom; x <= to.x+offsetTo; x++ {
			drawnCoords = append(drawnCoords, drawingCoord{x, from.y})
			(*d)[x][from.y] = horizontal
		}
	case UpperLeft:
		for x, y := from.x, from.y-offsetFrom; x >= to.x-offsetTo && y >= to.y-offsetTo; x, y = x-1, y-1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*d)[x][y] = falling
		}
	case UpperRight:
		for x, y := from.x, from.y-offsetFrom; x <= to.x+offsetTo && y >= to.y-offsetTo; x, y = x+1, y-1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*d)[x][y] = rising
		}
	case LowerLeft:
		for x, y := from.x, from.y+offsetFrom; x >= to.x-offsetTo && y <= to.y+offsetTo; x, y = x-1, y+1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*d)[x][y] = rising
		}
	case LowerRight:
		for x, y := from.x, from.y+offsetFrom; x <= to.x+offsetTo && y <= to.y+offsetTo; x, y = x+1, y+1 {
			drawnCoords = append(drawnCoords, drawingCoord{x, y})
			(*d)[x][y] = falling
		}
	}
	return drawnCoords
}

func drawMap(properties *graphProperties) (string, error) {
	g := mkGraph(properties.data, properties.nodeSpecs)
	g.setStyleClasses(properties)
	g.paddingX = properties.paddingX
	g.paddingY = properties.paddingY
	g.useAscii = properties.useAscii
	g.setSubgraphs(properties.subgraphs)
	if err := g.createMapping(); err != nil {
		return "", err
	}
	return drawingToString(g.draw()), nil
}

func drawBox(n *node, g *graph) *drawing {
	w := 0
	for i := range 2 {
		w += g.columnWidth[n.gridCoord.x+i]
	}
	h := 0
	for i := range 2 {
		h += g.rowHeight[n.gridCoord.y+i]
	}

	from := drawingCoord{0, 0}
	to := drawingCoord{w, h}
	boxDrawing := *(mkDrawing(Max(from.x, to.x), Max(from.y, to.y)))
	drawRectangleBorder(&boxDrawing, from, to, g.useAscii)
	innerTop := from.y + 1
	innerHeight := h - 1
	contentTop := innerTop + (innerHeight-n.label.contentHeight())/2
	for lineIdx, line := range n.label.lines {
		textY := contentTop + lineIdx*(graphLabelLineGap+1)
		textWidth := runewidth.StringWidth(line)
		textX := from.x + w/2 - CeilDiv(textWidth, 2) + 1
		for _, cell := range runewidth.Cells(line) {
			if cell != "" {
				cell = wrapTextInColor(cell, n.styleClass.styles["color"], g.styleType)
			}
			boxDrawing[textX][textY] = cell
			textX++
		}
	}

	return &boxDrawing
}

func drawSubgraph(sg *subgraph, g graph) *drawing {
	width := sg.maxX - sg.minX
	height := sg.maxY - sg.minY

	if width <= 0 || height <= 0 {
		return mkDrawing(0, 0)
	}

	from := drawingCoord{0, 0}
	to := drawingCoord{width, height}
	subgraphDrawing := *(mkDrawing(width, height))

	drawRectangleBorder(&subgraphDrawing, from, to, g.useAscii)
	return &subgraphDrawing
}

func drawRectangleBorder(target *drawing, from drawingCoord, to drawingCoord, useAscii bool) {
	horizontal, vertical := "─", "│"
	topLeft, topRight, bottomLeft, bottomRight := "┌", "┐", "└", "┘"
	if useAscii {
		horizontal, vertical = "-", "|"
		topLeft, topRight, bottomLeft, bottomRight = "+", "+", "+", "+"
	}

	for x := from.x + 1; x < to.x; x++ {
		(*target)[x][from.y] = horizontal
		(*target)[x][to.y] = horizontal
	}
	for y := from.y + 1; y < to.y; y++ {
		(*target)[from.x][y] = vertical
		(*target)[to.x][y] = vertical
	}
	(*target)[from.x][from.y] = topLeft
	(*target)[to.x][from.y] = topRight
	(*target)[from.x][to.y] = bottomLeft
	(*target)[to.x][to.y] = bottomRight
}

func drawSubgraphLabel(sg *subgraph) (*drawing, drawingCoord) {
	width := sg.maxX - sg.minX
	height := sg.maxY - sg.minY

	if width <= 0 || height <= 0 {
		return mkDrawing(0, 0), drawingCoord{0, 0}
	}

	from := drawingCoord{0, 0}
	to := drawingCoord{width, height}
	labelDrawing := *(mkDrawing(width, height))

	for lineIdx, line := range sg.label.lines {
		labelY := from.y + 1 + lineIdx*(graphLabelLineGap+1)
		labelX := max(from.x+width/2-runewidth.StringWidth(line)/2, from.x+1)
		for _, cell := range runewidth.Cells(line) {
			if labelX < to.x {
				labelDrawing[labelX][labelY] = cell
			}
			labelX++
		}
	}

	offset := drawingCoord{sg.minX, sg.minY}
	return &labelDrawing, offset
}

func wrapTextInColor(text, c, styleType string) string {
	if c == "" {
		return text
	}
	switch styleType {
	case "html":
		return fmt.Sprintf("<span style='color: %s'>%s</span>", c, text)
	case "cli":
		cliColor := color.HEX(c)
		return cliColor.Sprint(text)
	default:
		return text
	}
}

func (d *drawing) increaseSize(x int, y int) {
	currSizeX, currSizeY := getDrawingSize(d)
	drawingWithNewSize := mkDrawing(Max(x, currSizeX), Max(y, currSizeY))
	for x := range len(*drawingWithNewSize) {
		for y := range len((*drawingWithNewSize)[0]) {
			if x < len(*d) && y < len((*d)[0]) {
				(*drawingWithNewSize)[x][y] = (*d)[x][y]
			}
		}
	}
	*d = *drawingWithNewSize
}

func (g *graph) setDrawingSizeToGridConstraints() {
	maxX := 0
	maxY := 0
	for _, w := range g.columnWidth {
		maxX += w
	}
	for _, h := range g.rowHeight {
		maxY += h
	}
	g.drawing.increaseSize(maxX-1, maxY-1)
}

func mergeJunctions(c1, c2 string) string {
	junctionMap := map[string]map[string]string{
		"─": {"│": "┼", "┌": "┬", "┐": "┬", "└": "┴", "┘": "┴", "├": "┼", "┤": "┼", "┬": "┬", "┴": "┴"},
		"│": {"─": "┼", "┌": "├", "┐": "┤", "└": "├", "┘": "┤", "├": "├", "┤": "┤", "┬": "┼", "┴": "┼"},
		"┌": {"─": "┬", "│": "├", "┐": "┬", "└": "├", "┘": "┼", "├": "├", "┤": "┼", "┬": "┬", "┴": "┼"},
		"┐": {"─": "┬", "│": "┤", "┌": "┬", "└": "┼", "┘": "┤", "├": "┼", "┤": "┤", "┬": "┬", "┴": "┼"},
		"└": {"─": "┴", "│": "├", "┌": "├", "┐": "┼", "┘": "┴", "├": "├", "┤": "┼", "┬": "┼", "┴": "┴"},
		"┘": {"─": "┴", "│": "┤", "┌": "┼", "┐": "┤", "└": "┴", "├": "┼", "┤": "┤", "┬": "┼", "┴": "┴"},
		"├": {"─": "┼", "│": "├", "┌": "├", "┐": "┼", "└": "├", "┘": "┼", "┤": "┼", "┬": "┼", "┴": "┼"},
		"┤": {"─": "┼", "│": "┤", "┌": "┼", "┐": "┤", "└": "┼", "┘": "┤", "├": "┼", "┬": "┼", "┴": "┼"},
		"┬": {"─": "┬", "│": "┼", "┌": "┬", "┐": "┬", "└": "┼", "┘": "┼", "├": "┼", "┤": "┼", "┴": "┼"},
		"┴": {"─": "┴", "│": "┼", "┌": "┼", "┐": "┼", "└": "┴", "┘": "┴", "├": "┼", "┤": "┼", "┬": "┼"},
	}

	if merged, ok := junctionMap[c1][c2]; ok {
		return merged
	}

	return c1
}

func (g *graph) mergeDrawings(baseDrawing *drawing, mergeCoord drawingCoord, drawings ...*drawing) *drawing {
	maxX, maxY := getDrawingSize(baseDrawing)
	for _, d := range drawings {
		if d == nil {
			continue
		}
		dX, dY := getDrawingSize(d)
		maxX = Max(maxX, dX+mergeCoord.x)
		maxY = Max(maxY, dY+mergeCoord.y)
	}

	mergedDrawing := mkDrawing(maxX, maxY)

	for x := 0; x <= maxX; x++ {
		for y := 0; y <= maxY; y++ {
			if x < len(*baseDrawing) && y < len((*baseDrawing)[0]) {
				(*mergedDrawing)[x][y] = (*baseDrawing)[x][y]
			}
		}
	}

	for _, d := range drawings {
		if d == nil {
			continue
		}
		for x := range len(*d) {
			for y := range len((*d)[0]) {
				c := (*d)[x][y]
				if c != " " {
					currentChar := (*mergedDrawing)[x+mergeCoord.x][y+mergeCoord.y]
					if !g.useAscii && isJunctionChar(c) && isJunctionChar(currentChar) {
						(*mergedDrawing)[x+mergeCoord.x][y+mergeCoord.y] = mergeJunctions(currentChar, c)
					} else {
						(*mergedDrawing)[x+mergeCoord.x][y+mergeCoord.y] = c
					}
				}
			}
		}
	}

	return mergedDrawing
}

func isJunctionChar(c string) bool {
	return slices.Contains(junctionChars, c)
}

func drawingToString(d *drawing) string {
	maxX, maxY := getDrawingSize(d)
	dBuilder := strings.Builder{}
	for y := 0; y <= maxY; y++ {
		for x := 0; x <= maxX; x++ {
			dBuilder.WriteString((*d)[x][y])
		}
		if y != maxY {
			dBuilder.WriteString("\n")
		}
	}
	return dBuilder.String()
}

func mkDrawing(x int, y int) *drawing {
	d := make(drawing, x+1)
	for i := 0; i <= x; i++ {
		d[i] = make([]string, y+1)
		for j := 0; j <= y; j++ {
			d[i][j] = " "
		}
	}
	return &d
}

func copyCanvas(toBeCopied *drawing) *drawing {
	x, y := getDrawingSize(toBeCopied)
	return mkDrawing(x, y)
}

func getDrawingSize(d *drawing) (int, int) {
	return len(*d) - 1, len((*d)[0]) - 1
}

func determineDirection(from genericCoord, to genericCoord) direction {
	switch {
	case from.x == to.x:
		if from.y < to.y {
			return Down
		}
		return Up
	case from.y == to.y:
		if from.x < to.x {
			return Right
		}
		return Left
	case from.x < to.x:
		if from.y < to.y {
			return LowerRight
		}
		return UpperRight
	default:
		if from.y < to.y {
			return LowerLeft
		}
		return UpperLeft
	}
}
