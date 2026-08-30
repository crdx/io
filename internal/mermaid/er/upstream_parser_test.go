package er

import (
	"strings"
	"testing"
)

func TestIsErDiagram(t *testing.T) {
	for _, test := range []struct {
		in   string
		want bool
	}{
		{"erDiagram\n A ||--|| B : x", true},
		{"ERDIAGRAM\n A ||--|| B : x", true},
		{"erDiagramFoo\n A", false},
		{"graph TD\n A-->B", false},
		{"sequenceDiagram\n A->>B: x", false},
		{"", false},
	} {
		if got := IsErDiagram(test.in); got != test.want {
			t.Errorf("IsErDiagram(%q) = %v, want %v", test.in, got, test.want)
		}
	}
}

func TestParseRelationshipCardinalities(t *testing.T) {
	tests := []struct {
		token           string
		wantLeft        Cardinality
		wantRight       Cardinality
		wantIdentifying bool
	}{
		{"||--||", OnlyOne, OnlyOne, true},
		{"||--o{", OnlyOne, ZeroOrMore, true},
		{"}|--|{", OneOrMore, OneOrMore, true},
		{"|o..o|", ZeroOrOne, ZeroOrOne, false},
		{"}o..|{", ZeroOrMore, OneOrMore, false},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			diagram, err := Parse("erDiagram\n A " + tt.token + " B : rel")
			if err != nil {
				t.Fatalf("parse %q: %v", tt.token, err)
			}
			if len(diagram.Relationships) != 1 {
				t.Fatalf("want 1 relationship, got %d", len(diagram.Relationships))
			}
			relationship := diagram.Relationships[0]
			if relationship.LeftCard != tt.wantLeft || relationship.RightCard != tt.wantRight || relationship.Identifying != tt.wantIdentifying {
				t.Errorf("got L=%v R=%v ident=%v, want L=%v R=%v ident=%v",
					relationship.LeftCard, relationship.RightCard, relationship.Identifying, tt.wantLeft, tt.wantRight, tt.wantIdentifying)
			}
			if relationship.Left != "A" || relationship.Right != "B" || relationship.Label != "rel" {
				t.Errorf("endpoints/label wrong: %+v", relationship)
			}
		})
	}
}

func TestParseEntityAttributes(t *testing.T) {
	diagram, err := Parse("erDiagram\n USER {\n  int id PK\n  string email UK \"unique\"\n  bigint org_id FK\n  text bio\n }")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(diagram.Entities) != 1 {
		t.Fatalf("want 1 entity, got %d", len(diagram.Entities))
	}
	got := diagram.Entities[0].Attributes
	want := []Attribute{
		{Type: "int", Name: "id", Keys: []string{"PK"}},
		{Type: "string", Name: "email", Keys: []string{"UK"}, Comment: "unique"},
		{Type: "bigint", Name: "org_id", Keys: []string{"FK"}},
		{Type: "text", Name: "bio"},
	}
	if len(got) != len(want) {
		t.Fatalf("attrs = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Type != w.Type || g.Name != w.Name || g.Comment != w.Comment || len(g.Keys) != len(w.Keys) {
			t.Errorf("attr %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestParseErErrors(t *testing.T) {
	for _, test := range []struct{ name, in, wantErr string }{
		{"missing keyword", "A ||--|| B : x", "expected"},
		{"unclosed block", "erDiagram\n A {\n  int id", "unclosed"},
		{"bad attribute", "erDiagram\n A {\n  oneword\n }", "type and name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.in); err == nil {
				t.Errorf("expected error containing %q", test.wantErr)
			}
		})
	}
}

func TestEmptyErDiagram(t *testing.T) {
	d, err := Parse("erDiagram\n")
	if err != nil {
		t.Fatal(err)
	}
	if out := Render(d, false); strings.TrimSpace(out) != "" {
		t.Errorf("empty diagram should render empty, got %q", out)
	}
}
