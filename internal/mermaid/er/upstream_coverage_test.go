package er

import "testing"

func TestCardinalityMatrix(t *testing.T) {
	lefts := map[string]Cardinality{"||": OnlyOne, "|o": ZeroOrOne, "}o": ZeroOrMore, "}|": OneOrMore}
	rights := map[string]Cardinality{"||": OnlyOne, "o|": ZeroOrOne, "o{": ZeroOrMore, "|{": OneOrMore}
	for lt, lc := range lefts {
		for rt, rc := range rights {
			in := "erDiagram\n A " + lt + "--" + rt + " B : r"
			d, err := Parse(in)
			if err != nil {
				t.Fatalf("%q: %v", in, err)
			}
			r := d.Relationships[0]
			if r.LeftCard != lc || r.RightCard != rc || !r.Identifying {
				t.Errorf("%q => L=%v R=%v ident=%v", in, r.LeftCard, r.RightCard, r.Identifying)
			}
		}
	}
}

func TestIdentifyingVariants(t *testing.T) {
	cases := []struct {
		in          string
		identifying bool
	}{
		{"erDiagram\n A ||--|| B : r", true},
		{"erDiagram\n A ||..|| B : r", false},
		{"erDiagram\n A ||-.|| B : r", false},
		{"erDiagram\n A ||.-|| B : r", false},
		{"erDiagram\n A only one to one or more B : r", true},
		{"erDiagram\n A many optionally to zero or one B : r", false},
	}
	for _, test := range cases {
		d, err := Parse(test.in)
		if err != nil {
			t.Fatalf("%q: %v", test.in, err)
		}
		if d.Relationships[0].Identifying != test.identifying {
			t.Errorf("%q ident = %v, want %v", test.in, d.Relationships[0].Identifying, test.identifying)
		}
	}
}

func TestTextAliasCardinality(t *testing.T) {
	d, err := Parse("erDiagram\n A many to one or more B : r")
	if err != nil {
		t.Fatal(err)
	}
	r := d.Relationships[0]
	if r.LeftCard != ZeroOrMore || r.RightCard != OneOrMore {
		t.Errorf("got L=%v R=%v, want ZeroOrMore, OneOrMore", r.LeftCard, r.RightCard)
	}
}

func TestParenTypesAndMultiBlock(t *testing.T) {
	d, err := Parse("erDiagram\n T {\n  decimal(10, 2) amount\n }\n T {\n  varchar(255) name PK \"the name\"\n }")
	if err != nil {
		t.Fatal(err)
	}
	attributes := d.Entities[0].Attributes
	if len(attributes) != 2 {
		t.Fatalf("want 2 attrs (multi-block append), got %d", len(attributes))
	}
	if attributes[0].Type != "decimal(10, 2)" || attributes[0].Name != "amount" {
		t.Errorf("paren type parsed wrong: %+v", attributes[0])
	}
	if attributes[1].Type != "varchar(255)" || attributes[1].Name != "name" || len(attributes[1].Keys) != 1 || attributes[1].Comment != "the name" {
		t.Errorf("attr 2 wrong: %+v", attributes[1])
	}
}

func TestStyleLinesSkipped(t *testing.T) {
	d, err := Parse("erDiagram\n A ||--|| B : r\n classDef foo fill:#f00\n class A foo\n style B color:#0f0\n accTitle: My ER\n")
	if err != nil {
		t.Fatalf("style lines should be skipped, got: %v", err)
	}
	if len(d.Relationships) != 1 {
		t.Errorf("want 1 relationship, got %d", len(d.Relationships))
	}
}

func TestRecursiveAndDuplicate(t *testing.T) {
	diagram, err := Parse("erDiagram\n NODE ||--o{ NODE : parent\n NODE ||--|| NODE : self")
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Entities) != 1 {
		t.Errorf("recursive rels should not duplicate entity: got %d", len(diagram.Entities))
	}
	if len(diagram.Relationships) != 2 {
		t.Errorf("want 2 relationships, got %d", len(diagram.Relationships))
	}
}

func TestEitherSideCardinality(t *testing.T) {
	d, err := Parse("erDiagram\n grants o{--|| tx : has")
	if err != nil {
		t.Fatal(err)
	}
	r := d.Relationships[0]
	if r.LeftCard != ZeroOrMore || r.RightCard != OnlyOne {
		t.Errorf("got L=%v R=%v, want ZeroOrMore, OnlyOne", r.LeftCard, r.RightCard)
	}
}

func TestEntityAliases(t *testing.T) {
	cases := []struct{ in, id, display string }{
		{"erDiagram\n Signature draft {\n  int id\n }", "Signature", "draft"},
		{"erDiagram\n" + ` fua["Fresha User Account"] {` + "\n  int id\n }", "fua", "Fresha User Account"},
		{"erDiagram\n" + ` acct["Account Ledger"]`, "acct", "Account Ledger"},
	}
	for _, test := range cases {
		d, err := Parse(test.in)
		if err != nil {
			t.Fatalf("%q: %v", test.in, err)
		}
		e := d.Entities[0]
		if e.Name != test.id || e.Display != test.display {
			t.Errorf("%q => id=%q display=%q, want id=%q display=%q", test.in, e.Name, e.Display, test.id, test.display)
		}
	}
}

func TestDirectionAndLenientComments(t *testing.T) {
	in := "erDiagram\n direction LR\n T {\n  // a note line\n  string s \"unclosed comment\n  int n\n }"
	d, err := Parse(in)
	if err != nil {
		t.Fatalf("expected tolerant parse, got: %v", err)
	}
	if len(d.Entities) != 1 || len(d.Entities[0].Attributes) != 2 {
		t.Errorf("want 1 entity with 2 attrs, got %d entities", len(d.Entities))
	}
}
