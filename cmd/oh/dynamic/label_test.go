package dynamic_test

import (
	"testing"

	"crdx.org/io/cmd/oh/dynamic"
	"crdx.org/io/cmd/oh/style"
)

func label() dynamic.Label {
	return dynamic.Label{Name: "grep", Subject: "hello", Qualifier: "in internal"}
}

func check(t *testing.T, room int, name string, subject string, qualifier string) {
	t.Helper()

	elidedLabel := label().Elide(room)

	if elidedLabel.Name != name || elidedLabel.Subject != subject || elidedLabel.Qualifier != qualifier {
		t.Errorf(
			"in %d columns expected %q %q %q, got %q %q %q",
			room, name, subject, qualifier, elidedLabel.Name, elidedLabel.Subject, elidedLabel.Qualifier,
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
	elidedLabel := dynamic.Label{Name: "ls", Subject: "internal"}.Elide(80)

	if elidedLabel.Name != "ls" || elidedLabel.Subject != "internal" || elidedLabel.Qualifier != "" {
		t.Errorf("expected the label to stand, got %q %q %q", elidedLabel.Name, elidedLabel.Subject, elidedLabel.Qualifier)
	}
}

func TestALabelIsCutToTheCellsItHasRatherThanTheCharacters(t *testing.T) {
	elidedLabel := dynamic.Label{Name: "read", Subject: "日本語です"}.Elide(11)

	if elidedLabel.Name != "read" {
		t.Errorf("expected the name to survive, got %q", elidedLabel.Name)
	}

	if elidedLabel.Subject != "日本…" {
		t.Errorf("expected two characters and an ellipsis, got %q", elidedLabel.Subject)
	}

	if got := style.Width(elidedLabel.Name + " " + elidedLabel.Subject); got != 10 {
		t.Errorf("expected the label to measure 10 cells, got %d", got)
	}
}
