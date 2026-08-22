package input

import (
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/edit"
	"crdx.org/io/cmd/oh/style"
)

func TestTheRuleIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 40, 100} {
		for _, label := range []string{"", "gpt ⠶ 6 tools ⠶ io", strings.Repeat("wide", 40)} {
			rule := Ruler{Right: label}
			if got := style.Width(rule.render(width)); got != width {
				t.Errorf("expected a rule of %d columns, got %d", width, got)
			}
		}
	}
}

func TestTheLabelSitsAtTheRightHandEndOfTheRule(t *testing.T) {
	rule := Ruler{Right: style.Subtle("here")}

	want := " " + style.Subtle("here") + " " + style.Rule("──")
	if got := rule.render(20); !strings.HasSuffix(got, want) {
		t.Errorf("expected %q to end in %q", got, want)
	}
}

func TestARuleWithBothLabelsIsExactlyAsWideAsTheScreen(t *testing.T) {
	for _, width := range []int{0, 1, 20, 40, 100} {
		rule := Ruler{Left: "↑ 12", Right: "gpt ⠶ io"}
		if got := style.Width(rule.render(width)); got != width {
			t.Errorf("expected a rule of %d columns, got %d", width, got)
		}
	}
}

func TestTheLeftLabelIsDroppedFirstWhenTheRightIsKept(t *testing.T) {
	rule := Ruler{Left: "↑ 12", Right: "gpt ⠶ io"}

	got := rule.render(18)
	if strings.Contains(got, "12") {
		t.Errorf("expected the left label to be dropped, got %q", got)
	}
	if !strings.Contains(got, "gpt ⠶ io") {
		t.Errorf("expected the right label to be kept, got %q", got)
	}
}

func TestALabelTooWideForTheScreenIsDropped(t *testing.T) {
	rule := Ruler{Right: "far too long"}

	if got := rule.render(5); strings.Contains(got, "far") {
		t.Errorf("expected the label to be dropped, got %q", got)
	}
}

func TestTheBottomRuleCarriesALabelAtEitherEnd(t *testing.T) {
	block := Block{Bottom: Ruler{Left: "⠶ ─ io ─ gpt", Right: "↓ 2"}}

	got := style.Plain(bottomRuleOf(block, 40))
	if !strings.HasPrefix(got, "─ ⠶ ─ io ─ gpt ") {
		t.Errorf("expected the label at the left, got %q", got)
	}
	if !strings.HasSuffix(got, " ↓ 2 ──") {
		t.Errorf("expected the scroll marker at the right, got %q", got)
	}
}

func TestTheBottomRuleDropsItsRightLabelToKeepItsLeftOne(t *testing.T) {
	block := Block{Bottom: Ruler{Left: "⠶ ─ io ─ gpt", Right: "↓ 200"}}

	got := style.Plain(bottomRuleOf(block, 20))
	if !strings.Contains(got, "⠶ ─ io ─ gpt") {
		t.Errorf("expected the left label to survive, got %q", got)
	}
	if strings.Contains(got, "200") {
		t.Errorf("expected the scroll marker to give way, got %q", got)
	}
}

func bottomRuleOf(block Block, width int) string {
	rows, _, _ := block.Rows(width)

	return rows[len(rows)-1]
}

func TestALabelPaintedDownToNothingCostsNothing(t *testing.T) {
	bare := Ruler{Right: "here"}
	painted := Ruler{Left: style.Scrolled(""), Right: "here"}

	if want, got := bare.render(20), painted.render(20); want != got {
		t.Errorf("expected an empty painted label to be ignored, got %q rather than %q", got, want)
	}
}

func TestTheBlockFramesTheInputBetweenItsRules(t *testing.T) {
	block := Block{
		Top:    Ruler{Left: "↑ 3"},
		Input:  edit.Frame{Rows: []string{"one", "two"}, Row: 1, Column: 2},
		Bottom: Ruler{Left: "⠶ ─ io"},
	}

	rows, cursorRow, cursorColumn := block.Rows(40)

	if len(rows) != 4 {
		t.Fatalf("expected a rule either side of two input rows, got %d rows", len(rows))
	}
	if rows[1] != "one" || rows[2] != "two" {
		t.Errorf("expected the input rows between the rules, got %q", rows)
	}
	if cursorRow != 2 {
		t.Errorf("expected the cursor a row below the top rule, got %d", cursorRow)
	}
	if cursorColumn != 2 {
		t.Errorf("expected the cursor column to be carried over, got %d", cursorColumn)
	}
}
