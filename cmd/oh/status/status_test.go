package status_test

import (
	"testing"

	"crdx.org/io/cmd/oh/status"
	"crdx.org/io/cmd/oh/theme"
)

func label() status.Label {
	return status.Label{Name: "grep", Args: "hello", Detail: "in internal"}
}

func check(t *testing.T, room int, name string, args string, detail string) {
	t.Helper()

	elidedLabel := label().Elide(room)

	if elidedLabel.Name != name || elidedLabel.Args != args || elidedLabel.Detail != detail {
		t.Errorf(
			"in %d columns expected %q %q %q, got %q %q %q",
			room, name, args, detail, elidedLabel.Name, elidedLabel.Args, elidedLabel.Detail,
		)
	}
}

func TestALabelThatFitsIsLeftAlone(t *testing.T) {
	check(t, 22, "grep", "hello", "in internal")
	check(t, 80, "grep", "hello", "in internal")
}

func TestWhatQualifiesTheArgumentsIsCutFirst(t *testing.T) {
	check(t, 21, "grep", "hello", "in intern…")
	check(t, 12, "grep", "hello", "…")
}

func TestWhatQualifiesTheArgumentsGoesBeforeTheArgumentsAreCut(t *testing.T) {
	check(t, 10, "grep", "hello", "")
	check(t, 9, "grep", "hel…", "")
}

func TestTheNameIsTheLastToGo(t *testing.T) {
	check(t, 5, "grep", "", "")
	check(t, 3, "gr…", "", "")
	check(t, 1, "…", "", "")
	check(t, 0, "", "", "")
}

func TestALabelWithNothingQualifyingItIsUnaffected(t *testing.T) {
	elidedLabel := status.Label{Name: "ls", Args: "internal"}.Elide(80)

	if elidedLabel.Name != "ls" || elidedLabel.Args != "internal" || elidedLabel.Detail != "" {
		t.Errorf("expected the label to stand, got %q %q %q", elidedLabel.Name, elidedLabel.Args, elidedLabel.Detail)
	}
}

// A row has to stay on the line it was printed on, so a label is cut to the cells it has rather
// than the characters, and a two-cell character that would straddle the end is left out whole.
func TestALabelIsCutToTheCellsItHasRatherThanTheCharacters(t *testing.T) {
	elidedLabel := status.Label{Name: "read", Args: "日本語です"}.Elide(11)

	if elidedLabel.Name != "read" {
		t.Errorf("expected the name to survive, got %q", elidedLabel.Name)
	}

	if elidedLabel.Args != "日本…" {
		t.Errorf("expected two characters and an ellipsis, got %q", elidedLabel.Args)
	}

	if got := theme.Width(elidedLabel.Name + " " + elidedLabel.Args); got != 10 {
		t.Errorf("expected the label to measure 10 cells, got %d", got)
	}
}
