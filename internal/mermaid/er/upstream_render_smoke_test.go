package er

import (
	"strings"
	"testing"
)

func TestRenderEntity(t *testing.T) {
	d, err := Parse("erDiagram\n USER {\n  int id PK\n  string email UK \"unique\"\n  text bio\n }")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := strings.Join(renderEntity(d.Entities[0], unicodeGlyphs, 0), "\n")

	for _, want := range []string{"USER", "id", "int", "PK", "email", "unique", "bio"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, "┌") || !strings.HasSuffix(out, "┘") {
		t.Errorf("expected a bordered box:\n%s", out)
	}

	d2, _ := Parse("erDiagram\n T {\n  int a\n  int b\n }")
	out2 := strings.Join(renderEntity(d2.Entities[0], unicodeGlyphs, 0), "\n")
	if strings.Count(out2, "┬") != 1 {
		t.Errorf("expected exactly two columns (type,name):\n%s", out2)
	}
}
