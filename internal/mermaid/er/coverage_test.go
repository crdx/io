package er

import (
	"strings"
	"testing"
)

func TestEntityRelationshipDetectionAndParserEdgeCases(t *testing.T) {
	for source, want := range map[string]bool{
		"":                              false,
		"\n%% comment\nERDIAGRAM extra": true,
		"erDiagrammer":                  false,
		"graph LR":                      false,
	} {
		if got := IsErDiagram(source); got != want {
			t.Errorf("IsErDiagram(%q) = %v, want %v", source, got, want)
		}
	}

	source := "erDiagram\n\naccTitle: title\naccDescr {\nignored\n}\ndirection TB\nA:::hot {}\nB [Bee]\nC alias\nclass A hot"
	if _, err := Parse(source); err != nil {
		t.Fatalf("edge syntax: %v", err)
	}
	if _, _, err := parseAttributeBlock([]string{"", "// comment", "int id}"}, 0); err != nil {
		t.Fatalf("attribute block: %v", err)
	}
	for _, attribute := range []string{`string name "unterminated`, "decimal(10, 2) amount PK, FK", "`geo point` `two words`"} {
		_, _ = parseAttribute(attribute)
	}
	for _, line := range []string{`A:::hot "quoted:::safe"`, `"quoted %% safe" %% comment`, "plain"} {
		_ = stripClassShorthand(line)
		_ = stripComment(line)
	}
	_ = firstNonEmpty("a", "b")
	_ = firstNonEmpty("", "b")

	diagram := &ErDiagram{byName: map[string]*Entity{}}
	for _, line := range []string{
		"missing colon",
		"A nonsense B: x",
		": empty",
		"A xx--yy B: bad",
		"A ?? B: bad",
		`"unterminated ||--|| B : bad`,
		`A ||--|| "unterminated : bad`,
	} {
		_ = diagram.parseRelationship(line)
	}
	for _, part := range []string{"", `"unterminated`, `"quoted" ||`, "A ||"} {
		_, _ = splitEntityCard(part, true)
		_, _ = splitEntityCard(part, false)
	}
	_, _ = findWordOp("none")
	_ = splitAttrTokens("type name(unclosed `tick value`")
}

func TestEntityRelationshipCanvasAndRoutingEdgeCases(t *testing.T) {
	canvas := &canvas{}
	canvas.set(-1, 0, "x")
	canvas.set(0, -1, "x")
	canvas.stamp(0, 0, []string{"資料", "x"})
	if !strings.Contains(canvas.string(), "資料") {
		t.Fatal("wide stamp was not rendered")
	}

	overlay := newOverlay()
	overlay.polyline([][2]int{{2, 2}, {0, 2}, {0, 0}, {2, 0}, {2, 2}, {2, 2}}, true)
	overlay.polyline([][2]int{{0, 0}, {1, 1}}, false)
	_ = overlay.bits(0, 0)

	entity := &placedEntity{x: 1, y: 1, w: 12, h: 5, row: 0, col: 0}
	for _, side := range []side{sideT, sideB} {
		for index := range 4 {
			_, _ = attach(entity, side, index, 4)
		}
	}

	layout := &layout{vGutX: []int{0, 20}, hGutY: []int{0, 10, 20}, gutW: 8, lanes: 2}
	other := &placedEntity{x: 20, y: 12, w: 12, h: 5, row: 1, col: 1}
	first := endpoint{p: entity, s: sideB, x: 4, y: 5, card: OnlyOne}
	nonMerged := newPlan(layout, first, endpoint{p: other, s: sideB, x: 24, y: 16, card: ZeroOrMore}, &Relationship{Label: "long label", Identifying: true}, 0)
	for _, plan := range []routePlan{
		nonMerged,
		{rel: &Relationship{Label: "reverse runs"}, a: endpoint{p: entity, x: 17, y: 0}, b: endpoint{p: other, x: 0, y: 10}, ya: 3, yb: 8, tx: 18},
		{rel: &Relationship{Label: "merged"}, a: first, b: endpoint{p: other, s: sideB, x: 24, y: 5, card: OneOrMore}, ya: 8, yb: 8, merged: true},
		{rel: &Relationship{Label: "self"}, a: first, b: endpoint{p: entity, s: sideB, x: 10, y: 5, card: ZeroOrOne}, ya: 8, yb: 8, merged: true},
	} {
		plan.drawLine(overlay)
		plan.decorate(overlay)
	}

	for _, cardinality := range []Cardinality{OnlyOne, ZeroOrOne, ZeroOrMore, OneOrMore} {
		putToken(overlay, endpoint{x: 2, card: cardinality}, 10, 3)
		putToken(overlay, endpoint{x: 10, card: cardinality}, 2, 4)
		_ = leftToken(cardinality)
		_ = rightToken(cardinality)
	}
	putLabel(overlay, "", [][3]int{{0, 2, 0}})
	putLabel(overlay, "impossible", [][3]int{{0, 2, 0}})
	putLabel(overlay, "too wide", [][3]int{{0, 8, 0}})
	putLabel(overlay, "fit", [][3]int{{0, 20, 0}})
	writeLabel(overlay, "資料", 0, 0, 5)
	writeLabel(overlay, "a b", 0, 0, -1)
	crossings := newOverlay()
	for x := 4; x <= 6; x++ {
		crossings.solid[[2]int{x, 0}] = dN | dS
	}
	_, _ = labelStart(crossings, 0, 10, 3, 0)
	_, _ = labelStart(crossings, 0, 10, 8, 0)
	_, _ = labelStart(overlay, 0, 10, 3, 0)
	_ = vCrossings(overlay, 0, 10, 0)

	canvas.set(0, 0, "X")
	overlay.solid[[2]int{99, 99}] = 0
	composite(canvas, overlay, unicodeGlyphs)
	setAttachTee(canvas, endpoint{x: 0, y: 0, s: sideT}, unicodeGlyphs)
	canvas.set(1, 1, string(unicodeGlyphs.teeD))
	setAttachTee(canvas, endpoint{x: 1, y: 1, s: sideT}, unicodeGlyphs)

	for _, value := range []int{-2, 0, 2} {
		_ = abs(value)
		_ = sign(value)
	}
}

func TestEntityRelationshipConnectorAndPlacementEdgeCases(t *testing.T) {
	diagram := &ErDiagram{
		Entities: []*Entity{{Name: "A", Display: "A"}, {Name: "B", Display: "B"}, {Name: "C", Display: "C"}},
		Relationships: []*Relationship{
			{Left: "A", Right: "A", LeftCard: OnlyOne, RightCard: ZeroOrMore, Label: "self"},
			{Left: "A", Right: "B", LeftCard: ZeroOrOne, RightCard: OneOrMore, Label: "ab"},
			{Left: "missing", Right: "C", Label: "ignored"},
		},
	}
	layout := placeEntities(diagram, unicodeGlyphs)
	canvas := &canvas{}
	for _, entity := range layout.placed {
		canvas.stamp(entity.x, entity.y, entity.lines)
	}
	drawConnectors(canvas, layout, diagram, unicodeGlyphs)
	if canvas.string() == "" {
		t.Fatal("connectors rendered nothing")
	}

	empty := placeEntities(&ErDiagram{}, asciiGlyphs)
	if len(empty.placed) != 0 {
		t.Fatalf("empty layout has %d entities", len(empty.placed))
	}
	_ = blockWidth([]string{"a", "wide"})
	_ = renderEntity(&Entity{Display: "wide", Attributes: []Attribute{{Type: "int", Name: "id"}}}, unicodeGlyphs, 40)
}
