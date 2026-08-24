package er

import (
	"math"
	"strings"

	"crdx.org/io/internal/mermaid/runewidth"
)

type canvas struct {
	rows [][]string
}

func (c *canvas) ensure(x, y int) {
	for len(c.rows) <= y {
		c.rows = append(c.rows, nil)
	}
	for len(c.rows[y]) <= x {
		c.rows[y] = append(c.rows[y], " ")
	}
}

func (c *canvas) set(x, y int, cell string) {
	if x < 0 || y < 0 {
		return
	}
	c.ensure(x, y)
	c.rows[y][x] = cell
}

func (c *canvas) at(x, y int) string {
	if y < 0 || y >= len(c.rows) || x < 0 || x >= len(c.rows[y]) {
		return " "
	}
	return c.rows[y][x]
}

func (c *canvas) stamp(x0, y0 int, block []string) {
	for dy, line := range block {
		for x, cell := range runewidth.Cells(line) {
			c.set(x0+x, y0+dy, cell)
		}
	}
}

func (c *canvas) string() string {
	var b strings.Builder
	for _, row := range c.rows {
		b.WriteString(strings.TrimRight(strings.Join(row, ""), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

type side int

const (
	sideT side = iota
	sideB
)

type placedEntity struct {
	entity   *Entity
	lines    []string
	x, y     int // top-left
	w, h     int // box dimensions
	row, col int // grid cell
}

type layout struct {
	byName map[string]*placedEntity
	placed []*placedEntity
	lanes  int   // one lane per relationship (global, so lanes never clash)
	gutW   int   // width of each vertical gutter
	vGutX  []int // left edge x of each vertical gutter (len cols+1)
	hGutY  []int // top edge y of each horizontal gutter (len rows+1)
}

func placeEntities(d *ErDiagram, g glyphs) *layout {
	n := len(d.Entities)
	cols := max(int(math.Ceil(math.Sqrt(float64(n)))), 1)
	rows := (n + cols - 1) / cols

	lanes := max(len(d.Relationships), 1)

	maxLabel := 0
	for _, r := range d.Relationships {
		if w := runewidth.StringWidth(r.Label); w > maxLabel {
			maxLabel = w
		}
	}
	gutW := lanes + maxLabel + 5

	deg := map[string]int{}
	selfLoop := map[string]bool{}
	for _, r := range d.Relationships {
		deg[r.Left]++
		deg[r.Right]++
		if r.Left == r.Right {
			selfLoop[r.Left] = true
		}
	}

	placed := make([]*placedEntity, n)
	for i, e := range d.Entities {
		minW := 4*deg[e.Name] + 1
		if selfLoop[e.Name] {
			minW = 11
			if deg[e.Name] > 2 {
				minW = 4*deg[e.Name] + 9
			}
		}
		lines := renderEntity(e, g, minW-2)
		placed[i] = &placedEntity{
			entity: e, lines: lines,
			w: blockWidth(lines), h: len(lines),
			row: i / cols, col: i % cols,
		}
	}

	colW := make([]int, cols)
	rowH := make([]int, rows)
	for _, p := range placed {
		if p.w > colW[p.col] {
			colW[p.col] = p.w
		}
		if p.h > rowH[p.row] {
			rowH[p.row] = p.h
		}
	}

	vGutX := make([]int, cols+1)
	colX := make([]int, cols)
	x := 0
	for c := range cols {
		vGutX[c] = x
		if c > 0 {
			x += gutW
			colX[c] = x
		} else {
			colX[0] = 0
		}
		x += colW[c]
	}
	vGutX[cols] = x

	hGutY := make([]int, rows+1)
	rowY := make([]int, rows)
	y := 0
	for r := range rows {
		hGutY[r] = y
		if r > 0 {
			y += lanes
			rowY[r] = y
		} else {
			rowY[0] = 0
		}
		y += rowH[r]
	}
	hGutY[rows] = y

	byName := map[string]*placedEntity{}
	for _, p := range placed {
		p.x, p.y = colX[p.col], rowY[p.row]
		byName[p.entity.Name] = p
	}
	return &layout{byName: byName, placed: placed, lanes: lanes, gutW: gutW, vGutX: vGutX, hGutY: hGutY}
}

func blockWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		if lw := runewidth.StringWidth(l); lw > w {
			w = lw
		}
	}
	return w
}

const (
	dN uint8 = 1 << iota
	dS
	dE
	dW
)

type overlay struct {
	solid map[[2]int]uint8
	dash  map[[2]int]uint8
	label map[[2]int]string
	token map[[2]int]string
}

func newOverlay() *overlay {
	return &overlay{
		solid: map[[2]int]uint8{}, dash: map[[2]int]uint8{},
		label: map[[2]int]string{}, token: map[[2]int]string{},
	}
}

func (o *overlay) bits(x, y int) uint8 { return o.solid[[2]int{x, y}] | o.dash[[2]int{x, y}] }

func (o *overlay) polyline(pts [][2]int, solid bool) {
	m := o.dash
	if solid {
		m = o.solid
	}
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		dx, dy := sign(b[0]-a[0]), sign(b[1]-a[1])
		x, y := a[0], a[1]
		for x != b[0] || y != b[1] {
			if dx > 0 {
				m[[2]int{x, y}] |= dE
			} else if dx < 0 {
				m[[2]int{x, y}] |= dW
			}
			if dy > 0 {
				m[[2]int{x, y}] |= dS
			} else if dy < 0 {
				m[[2]int{x, y}] |= dN
			}
			x += dx
			y += dy
			if dx > 0 {
				m[[2]int{x, y}] |= dW
			} else if dx < 0 {
				m[[2]int{x, y}] |= dE
			}
			if dy > 0 {
				m[[2]int{x, y}] |= dN
			} else if dy < 0 {
				m[[2]int{x, y}] |= dS
			}
		}
	}
}

func attach(p *placedEntity, s side, idx, total int) (int, int) {
	lo, hi := p.x+1, p.x+p.w-2
	x := lo + (hi-lo-4*(total-1))/2 + 4*idx
	if (s == sideB) == (x%2 != 0) {
		x++
	}
	x = max(lo, min(x, hi))
	y := p.y + p.h - 1
	if s == sideT {
		y = p.y
	}
	return x, y
}

type endpoint struct {
	p    *placedEntity
	s    side
	x, y int
	card Cardinality
}

func drawConnectors(c *canvas, lay *layout, d *ErDiagram, g glyphs) {
	o := newOverlay()

	type ends struct{ a, b endpoint }
	all := make([]ends, len(d.Relationships))
	slotCount := map[[2]int]int{}
	entIdx := map[*placedEntity]int{}
	for i, p := range lay.placed {
		entIdx[p] = i
	}
	for i, r := range d.Relationships {
		a, b := lay.byName[r.Left], lay.byName[r.Right]
		if a == nil || b == nil {
			continue
		}
		sa, sb := sidesFor(a, b)
		all[i] = ends{
			a: endpoint{p: a, s: sa, card: r.LeftCard},
			b: endpoint{p: b, s: sb, card: r.RightCard},
		}
		slotCount[[2]int{entIdx[a], int(sa)}]++
		slotCount[[2]int{entIdx[b], int(sb)}]++
	}
	slotUsed := map[[2]int]int{}
	selfSeen := map[*placedEntity]int{}
	for i := range all {
		if all[i].a.p == nil {
			continue
		}
		for _, ep := range []*endpoint{&all[i].a, &all[i].b} {
			key := [2]int{entIdx[ep.p], int(ep.s)}
			ep.x, ep.y = attach(ep.p, ep.s, slotUsed[key], slotCount[key])
			slotUsed[key]++
		}
		if p := all[i].a.p; p == all[i].b.p {
			in := 4 * selfSeen[p]
			selfSeen[p]++
			x1, x2 := p.x+1+in, p.x+p.w-2-in
			if x1%2 != 0 {
				x1++
			}
			if x2%2 != 0 {
				x2--
			}
			all[i].a.x, all[i].b.x = x1, x2
		}
	}

	plans := make([]routePlan, 0, len(d.Relationships))
	for i, r := range d.Relationships {
		if all[i].a.p == nil {
			continue
		}
		plans = append(plans, newPlan(lay, all[i].a, all[i].b, r, i))
	}

	for _, p := range plans {
		p.drawLine(o)
	}
	for _, p := range plans {
		p.decorate(o)
	}

	composite(c, o, g)

	for _, p := range plans {
		setAttachTee(c, p.a, g)
		setAttachTee(c, p.b, g)
	}
}

func setAttachTee(c *canvas, ep endpoint, g glyphs) {
	tee, opposite := g.teeD, g.teeU
	if ep.s == sideT {
		tee, opposite = g.teeU, g.teeD
	}
	if c.at(ep.x, ep.y) == string(opposite) {
		tee = g.cross
	}
	c.set(ep.x, ep.y, string(tee))
}

func sidesFor(a, b *placedEntity) (side, side) {
	sameColAdjacent := a.col == b.col && abs(a.row-b.row) == 1
	switch {
	case a.row < b.row:
		if sameColAdjacent {
			return sideB, sideB
		}
		return sideB, sideT
	case a.row > b.row:
		if sameColAdjacent {
			return sideB, sideB
		}
		return sideT, sideB
	default:
		return sideB, sideB
	}
}

func (l *layout) gutterY(e endpoint, lane int) int {
	if e.s == sideB {
		return l.hGutY[e.p.row+1] + lane
	}
	return l.hGutY[e.p.row] + lane
}

func (l *layout) trunkX(a, b *placedEntity, lane int) int {
	return l.vGutX[min(a.col, b.col)+1] + l.gutW - l.lanes + lane
}

type routePlan struct {
	rel    *Relationship
	a, b   endpoint
	ya, yb int
	tx     int  // trunk column, valid only when !merged
	merged bool // both stubs meet one gutter row: single run, no trunk
}

func newPlan(lay *layout, a, b endpoint, r *Relationship, lane int) routePlan {
	ya, yb := lay.gutterY(a, lane), lay.gutterY(b, lane)
	p := routePlan{rel: r, a: a, b: b, ya: ya, yb: yb, merged: ya == yb}
	if !p.merged {
		p.tx = lay.trunkX(a.p, b.p, lane)
	}
	return p
}

func (p routePlan) drawLine(o *overlay) {
	if p.merged {
		o.polyline([][2]int{
			{p.a.x, p.a.y}, {p.a.x, p.ya}, {p.b.x, p.ya}, {p.b.x, p.b.y},
		}, p.rel.Identifying)
		return
	}
	o.polyline([][2]int{
		{p.a.x, p.a.y}, {p.a.x, p.ya}, {p.tx, p.ya}, {p.tx, p.yb}, {p.b.x, p.yb}, {p.b.x, p.b.y},
	}, p.rel.Identifying)
}

func (p routePlan) decorate(o *overlay) {
	if p.merged {
		putToken(o, p.a, p.b.x, p.ya)
		putToken(o, p.b, p.a.x, p.ya)
		if p.a.p == p.b.p {
			writeLabel(o, p.rel.Label, max(p.a.x, p.b.x)+2, p.ya, -1)
		} else {
			putLabel(o, p.rel.Label, [][3]int{{min(p.a.x, p.b.x), max(p.a.x, p.b.x), p.ya}})
		}
		return
	}
	putToken(o, p.a, p.tx, p.ya)
	putToken(o, p.b, p.tx, p.yb)
	runs := [][3]int{
		{min(p.a.x, p.tx), max(p.a.x, p.tx), p.ya},
		{min(p.b.x, p.tx), max(p.b.x, p.tx), p.yb},
	}
	if runs[1][1]-runs[1][0] > runs[0][1]-runs[0][0] {
		runs[0], runs[1] = runs[1], runs[0]
	}
	putLabel(o, p.rel.Label, runs)
}

func putToken(o *overlay, ep endpoint, targetX, y int) {
	if ep.x < targetX {
		for i, cell := range runewidth.Cells(leftToken(ep.card)) {
			o.token[[2]int{ep.x + 1 + i, y}] = cell
		}
		return
	}
	tokenCells := runewidth.Cells(rightToken(ep.card))
	for i, cell := range tokenCells {
		o.token[[2]int{ep.x - len(tokenCells) + i, y}] = cell
	}
}

func putLabel(o *overlay, s string, runs [][3]int) {
	if s == "" {
		return
	}
	lw := runewidth.StringWidth(s)
	type spot struct{ start, cost, y, hi int }
	best := spot{cost: -1}
	for _, r := range runs {
		lo, hi, y := r[0]+3, r[1]-3, r[2]
		if hi-lo+1 < lw {
			continue
		}
		start, cost := labelStart(o, lo, hi, lw, y)
		if best.cost < 0 || cost < best.cost {
			best = spot{start, cost, y, hi}
		}
		if cost == 0 {
			break
		}
	}
	if best.cost < 0 {
		lo, hi, y := runs[0][0]+3, runs[0][1]-3, runs[0][2]
		if lo > hi {
			return
		}
		start, _ := labelStart(o, lo, hi, lw, y)
		best = spot{start, 0, y, hi}
	}
	writeLabel(o, s, best.start, best.y, best.hi)
}

func labelStart(o *overlay, lo, hi, lw, y int) (int, int) {
	centre := max(lo, lo+(hi-lo+1-lw)/2)
	start, cost := centre, vCrossings(o, centre, min(centre+lw-1, hi), y)
	for d := 1; d <= hi-lo && cost > 0; d++ {
		for _, c := range []int{centre - d, centre + d} {
			if c < lo || c+lw-1 > hi {
				continue
			}
			if n := vCrossings(o, c, c+lw-1, y); n < cost {
				start, cost = c, n
			}
		}
	}
	return start, cost
}

func vCrossings(o *overlay, x0, x1, y int) int {
	n := 0
	for x := x0; x <= x1; x++ {
		if o.bits(x, y)&(dN|dS) != 0 {
			n++
		}
	}
	return n
}

func writeLabel(o *overlay, label string, x, y, limit int) {
	for _, cell := range runewidth.Cells(label) {
		if limit >= 0 && x > limit {
			return
		}
		if cell != " " || o.bits(x, y)&(dN|dS) == 0 {
			o.label[[2]int{x, y}] = cell
		}
		x++
	}
}

func composite(c *canvas, o *overlay, g glyphs) {
	seen := map[[2]int]bool{}
	mark := func(x, y int) {
		p := [2]int{x, y}
		if seen[p] {
			return
		}
		seen[p] = true
		bits := o.bits(x, y)
		if bits == 0 {
			return
		}
		if c.at(x, y) != " " {
			return
		}
		c.set(x, y, string(glyphFor(bits, o.solid[p] != 0, g)))
	}
	for p := range o.solid {
		mark(p[0], p[1])
	}
	for p := range o.dash {
		mark(p[0], p[1])
	}
	for p, r := range o.label {
		c.set(p[0], p[1], r)
	}
	for p, r := range o.token {
		c.set(p[0], p[1], r)
	}
}

func glyphFor(bits uint8, solid bool, g glyphs) rune {
	switch bits {
	case dN | dS:
		if solid {
			return g.v
		}
		return g.vd
	case dE | dW:
		if solid {
			return g.h
		}
		return g.hd
	case dN | dE:
		return g.bl
	case dN | dW:
		return g.br
	case dS | dE:
		return g.tl
	case dS | dW:
		return g.tr
	case dN | dS | dE:
		return g.teeR
	case dN | dS | dW:
		return g.teeL
	case dN | dE | dW:
		return g.teeU
	case dS | dE | dW:
		return g.teeD
	case dN | dS | dE | dW:
		return g.cross
	case dN, dS:
		if solid {
			return g.v
		}
		return g.vd
	default:
		if solid {
			return g.h
		}
		return g.hd
	}
}

func leftToken(c Cardinality) string {
	switch c {
	case OnlyOne:
		return "||"
	case ZeroOrOne:
		return "|o"
	case ZeroOrMore:
		return "}o"
	default:
		return "}|"
	}
}

func rightToken(c Cardinality) string {
	switch c {
	case OnlyOne:
		return "||"
	case ZeroOrOne:
		return "o|"
	case ZeroOrMore:
		return "o{"
	default:
		return "|{"
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func sign(n int) int {
	if n > 0 {
		return 1
	}
	if n < 0 {
		return -1
	}
	return 0
}
