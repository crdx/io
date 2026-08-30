package er

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestUnquotedAliasForms(t *testing.T) {
	diagram, err := Parse("erDiagram\n p[Person] {\n  string firstName\n }\n a[\"Customer Account\"] {\n  string email\n }\n p ||--o| a : has")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Entities) != 2 {
		t.Fatalf("want 2 entities, got %d", len(diagram.Entities))
	}
	if diagram.Entities[0].Display != "Person" || diagram.Entities[1].Display != "Customer Account" {
		t.Errorf("aliases not applied: %q, %q", diagram.Entities[0].Display, diagram.Entities[1].Display)
	}
	if diagram.Relationships[0].Left != "p" || diagram.Relationships[0].Right != "a" {
		t.Errorf("relationship should reference ids p/a, got %q/%q",
			diagram.Relationships[0].Left, diagram.Relationships[0].Right)
	}

	d2, err := Parse("erDiagram\n p[Person]\n a[\"Customer Account\"]")
	if err != nil {
		t.Fatal(err)
	}
	if d2.Entities[0].Display != "Person" || d2.Entities[1].Display != "Customer Account" {
		t.Errorf("lone aliases not applied: %q, %q", d2.Entities[0].Display, d2.Entities[1].Display)
	}
}

func TestQuotedEntityNameInRelationship(t *testing.T) {
	diagram, err := Parse("erDiagram\n \"ORDER ITEM\" {\n  int id\n }\n CUSTOMER ||--o{ \"ORDER ITEM\" : has\n \"ORDER ITEM\" ||--|| SKU : tracks")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Entities) != 3 {
		t.Fatalf("want 3 entities, got %d: quoted name split apart", len(diagram.Entities))
	}
	if diagram.Relationships[0].Right != "ORDER ITEM" || diagram.Relationships[1].Left != "ORDER ITEM" {
		t.Errorf("quoted name not matched to declaration: %+v", diagram.Relationships)
	}
}

func TestWideRuneAlignment(t *testing.T) {
	d, err := Parse("erDiagram\n 顧客 ||--o{ ORDER : 注文\n ORDER ||--|| LINE : has")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(Render(d, false), "\n")
	top := lines[0]
	for _, col := range []rune{'┌', '┐'} {
		if !strings.ContainsRune(top, col) {
			t.Fatalf("no %c in top border: %q", col, top)
		}
	}
	openCols := runeColumns(top, '┌')
	closeCols := runeColumns(lines[2], '└', '┴', '├')
	for _, c := range openCols {
		if !containsInt(closeCols, c) {
			t.Errorf("box corner at column %d misaligned (CJK width bug):\n%s",
				c, strings.Join(lines[:4], "\n"))
		}
	}
}

func TestSelfLoopTokensIntact(t *testing.T) {
	for _, name := range []string{"A", "ABC", "EMPLOYEE"} {
		d, err := Parse("erDiagram\n " + name + " ||--o{ " + name + " : manages")
		if err != nil {
			t.Fatal(err)
		}
		out := Render(d, false)
		if !strings.Contains(out, "||") || !strings.Contains(out, "o{") {
			t.Errorf("%s self-loop lost a token:\n%s", name, out)
		}
		if !strings.Contains(out, "manages") {
			t.Errorf("%s self-loop lost its label:\n%s", name, out)
		}
	}
}

func TestManyRelationshipsDistinctAttach(t *testing.T) {
	d, err := Parse("erDiagram\n A ||--o{ B : first\n A ||--o{ B : second\n B ||--|| A : third")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(Render(d, false), "\n")
	if n := strings.Count(lines[2], "┬"); n != 6 {
		t.Errorf("want 6 attach tees (3 per box), got %d:\n%s", n, lines[2])
	}
}

func TestEntityNamedClass(t *testing.T) {
	diagram, err := Parse("erDiagram\n class {\n  int id\n }\n class ||--|| B : has")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Entities) != 2 || len(diagram.Relationships) != 1 {
		t.Fatalf("entity named class mishandled: %d entities, %d relationships",
			len(diagram.Entities), len(diagram.Relationships))
	}
}

func TestParseErrorLineNumbers(t *testing.T) {
	_, err := Parse("erDiagram\n %% one\n %% two\n A ||--|| B : ok\n x : y : z")
	if err == nil || !strings.Contains(err.Error(), "line 5") {
		t.Errorf("want error at line 5, got %v", err)
	}
}

func TestAccDescrBlockSkipped(t *testing.T) {
	d, err := Parse("erDiagram\n accTitle: My title\n accDescr {\n a long\n description\n }\n A ||--|| B : has")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Entities) != 2 {
		t.Fatalf("accDescr block leaked entities: got %d", len(d.Entities))
	}
}

func TestLongLabelSurvivesManyLanes(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("erDiagram\n")
	for i := range 14 {
		builder.WriteString(" ")
		builder.WriteString(entityName(i % 9))
		builder.WriteString(" ||--|| ")
		builder.WriteString(entityName((i + 1) % 9))
		builder.WriteString(" : r\n")
	}
	builder.WriteString(" E0 ||--|| E7 : twenty-char-label-xx\n")
	d, err := Parse(builder.String())
	if err != nil {
		t.Fatal(err)
	}
	if out := Render(d, false); !strings.Contains(out, "twenty-char-label-xx") {
		t.Errorf("long label truncated:\n%s", out)
	}
}

func entityName(i int) string { return "E" + strconv.Itoa(i) }

func TestClassShorthandStripped(t *testing.T) {
	diagram, err := Parse("erDiagram\n A:::x\n B:::y,z {\n  int id\n }\n C[\"Custom C\"]:::w\n A:::x ||--o{ B:::y : links")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Entities) != 3 {
		t.Fatalf("want 3 entities, got %d: %+v", len(diagram.Entities), diagram.Entities)
	}
	for i, want := range []string{"A", "B", "C"} {
		if diagram.Entities[i].Name != want {
			t.Errorf("entity %d = %q, want %q (::: not stripped)", i, diagram.Entities[i].Name, want)
		}
	}
	if diagram.Entities[2].Display != "Custom C" {
		t.Errorf("alias lost when stripping :::, got %q", diagram.Entities[2].Display)
	}
	if r := diagram.Relationships[0]; r.Left != "A" || r.Right != "B" {
		t.Errorf("relationship endpoints = %q/%q, want A/B", r.Left, r.Right)
	}
	if out := Render(diagram, false); strings.Contains(out, ":::") {
		t.Errorf("::: leaked into render:\n%s", out)
	}
}

func TestSubgraphRejected(t *testing.T) {
	_, err := Parse("erDiagram\n subgraph CUSTOMERS\n  A ||--|| B : has\n end")
	if err == nil || !strings.Contains(err.Error(), "subgraph") {
		t.Errorf("want a subgraph-not-supported error, got %v", err)
	}
}

func TestBacktickAttributesStripped(t *testing.T) {
	diagram, err := Parse("erDiagram\n X {\n  type `geo.accuracy`\n  `geo point` `two words` PK\n }")
	if err != nil {
		t.Fatal(err)
	}
	attrs := diagram.Entities[0].Attributes
	want := [][2]string{{"type", "geo.accuracy"}, {"geo point", "two words"}}
	for i, w := range want {
		if attrs[i].Type != w[0] || attrs[i].Name != w[1] {
			t.Errorf("attr %d = %q %q, want %q %q", i, attrs[i].Type, attrs[i].Name, w[0], w[1])
		}
	}
	if len(attrs[1].Keys) != 1 || attrs[1].Keys[0] != "PK" {
		t.Errorf("PK key lost after backtick tokens: %+v", attrs[1])
	}
	if out := Render(diagram, false); strings.Contains(out, "`") {
		t.Errorf("backticks leaked into render:\n%s", out)
	}
}

func TestTwoSelfLoopsDistinct(t *testing.T) {
	d, err := Parse("erDiagram\n E ||--o{ E : first\n E }o..o| E : second")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(Render(d, false), "\n")
	if n := strings.Count(lines[2], "┬"); n != 4 {
		t.Errorf("want 4 distinct attach tees, got %d:\n%s", n, lines[2])
	}
	out := strings.Join(lines, "\n")
	for _, want := range []string{"first", "second"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing label %q:\n%s", want, out)
		}
	}
}

func TestWhitespaceLabelsCollapse(t *testing.T) {
	diagram, err := Parse("erDiagram\n A ||--o{ B : \"   \"\n A ||--o{ C : \"     x\"")
	if err != nil {
		t.Fatal(err)
	}
	if diagram.Relationships[0].Label != "" || diagram.Relationships[1].Label != "x" {
		t.Fatalf("labels not collapsed: %q, %q",
			diagram.Relationships[0].Label, diagram.Relationships[1].Label)
	}
	for line := range strings.SplitSeq(Render(diagram, false), "\n") {
		if _, after, found := strings.Cut(line, "─ "); found && strings.Contains(after, "─") {
			t.Errorf("hole punched in connector: %q", line)
		}
	}
}

func runeColumns(s string, targets ...rune) []int {
	var cols []int
	col := 0
	for _, r := range s {
		for _, t := range targets {
			if r == t {
				cols = append(cols, col)
			}
		}
		col++
	}
	return cols
}

func containsInt(values []int, value int) bool {
	return slices.Contains(values, value)
}
