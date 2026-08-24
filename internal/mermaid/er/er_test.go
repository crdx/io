package er

import (
	"strings"
	"testing"
)

func TestEverySupportedEntityRelationshipConstructParsesAndRenders(t *testing.T) {
	source := "erDiagram\ndirection LR\n%% ignored\nCUSTOMER[Customer account]:::hot {\nint id PK\nstring email UK \"login address\"\nint owner_id FK\ndecimal(10, 2) balance\n}\nORDER {}\nCUSTOMER ||--o{ ORDER : places\nORDER |o..o| LINE_ITEM : optionally contains\nLINE_ITEM }|--|{ PRODUCT : references\nPRODUCT ||--|| PRODUCT : replaces\nclassDef hot color:#fff\nclass CUSTOMER hot\nstyle ORDER color:#000"
	parsed, err := Parse(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, useASCII := range []bool{false, true} {
		output := Render(parsed, useASCII)
		for _, want := range []string{"Customer account", "email", "UK", "login address", "owner_id", "FK", "balance", "places", "optionally contains", "references", "replaces"} {
			if !strings.Contains(output, want) {
				t.Errorf("ascii=%v: expected %q in output", useASCII, want)
			}
		}
	}
}

func TestEntityRelationshipParserAcceptsEveryCardinalitySpelling(t *testing.T) {
	cards := []string{
		"||", "|o", "o|", "}o", "o{", "}|", "|{",
		"1", "only one", "one", "zero or one", "one or zero",
		"0+", "zero or more", "zero or many", "many", "many(0)",
		"1+", "one or more", "one or many", "many(1)",
	}
	for _, left := range cards {
		for _, right := range cards {
			source := "erDiagram\nA " + left + " to " + right + " B : relation"
			parsed, err := Parse(source)
			if err != nil {
				t.Fatalf("%q to %q: unexpected error: %v", left, right, err)
			}
			if len(parsed.Relationships) != 1 {
				t.Fatalf("%q to %q: got %d relationships", left, right, len(parsed.Relationships))
			}
		}
	}
}

func TestEntityRelationshipParserRejectsEveryInvalidStatementClass(t *testing.T) {
	for name, source := range map[string]string{
		"wrong keyword":       "graph LR\nA --> B",
		"subgraph":            "erDiagram\nsubgraph group\nA\nend",
		"orphan end":          "erDiagram\nend",
		"unclosed attributes": "erDiagram\nA {\nint id",
		"short attribute":     "erDiagram\nA {\nint\n}",
		"invalid keys":        "erDiagram\nA {\nint id NOPE\n}",
		"invalid statement":   "erDiagram\nA ??? B",
	} {
		if _, err := Parse(source); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestEntityRelationshipParserHandlesQuotedAndBacktickTokens(t *testing.T) {
	source := "erDiagram\n\"ORDER ITEM\" {\n`geo point` `two words` PK,FK \"position\"\n}\n\"ORDER ITEM\" only one optionally to zero or more \"LINE ITEM\" : contains"
	parsed, err := Parse(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := Render(parsed, false)
	for _, want := range []string{"ORDER ITEM", "LINE ITEM", "geo point", "two words", "PK,FK", "position", "contains"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}

func TestEntityRelationshipRoutingCoversEveryJunctionAndPlacement(t *testing.T) {
	for bits := range 16 {
		for _, solid := range []bool{false, true} {
			if got := glyphFor(uint8(bits), solid, unicodeGlyphs); got == 0 {
				t.Errorf("bits=%04b solid=%v: got an empty glyph", bits, solid)
			}
		}
	}

	layout := &layout{vGutX: []int{0, 10, 20}, gutW: 5, lanes: 2}
	left := &placedEntity{row: 0, col: 0}
	right := &placedEntity{row: 0, col: 1}
	if got := layout.trunkX(left, right, 1); got != 14 {
		t.Errorf("got trunk column %d, want 14", got)
	}

	positions := [][2]*placedEntity{
		{{row: 0, col: 0}, {row: 1, col: 0}},
		{{row: 1, col: 0}, {row: 0, col: 0}},
		{{row: 0, col: 0}, {row: 2, col: 0}},
		{{row: 2, col: 0}, {row: 0, col: 0}},
		{{row: 0, col: 0}, {row: 0, col: 1}},
	}
	for _, pair := range positions {
		sidesFor(pair[0], pair[1])
	}
}

func TestEmptyEntityRelationshipDiagramRendersEmpty(t *testing.T) {
	parsed, err := Parse("erDiagram\n%% nothing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output := Render(parsed, false); output != "" {
		t.Errorf("got %q, want empty output", output)
	}
}
