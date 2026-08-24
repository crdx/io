package roundrobin

import "testing"

func TestNextStartsAtTheBeginningAndWraps(t *testing.T) {
	entries := []string{"first", "second"}

	if got := Next(entries, ""); got != "first" {
		t.Errorf("got %q, want first", got)
	}
	if got := Next(entries, "first"); got != "second" {
		t.Errorf("got %q, want second", got)
	}
	if got := Next(entries, "second"); got != "first" {
		t.Errorf("got %q, want first", got)
	}
}

func TestNextStartsOverWhenTheLastEntryWasRemoved(t *testing.T) {
	if got := Next([]string{"second", "third"}, "first"); got != "second" {
		t.Errorf("got %q, want second", got)
	}
}

func TestNextReturnsTheOnlyEntry(t *testing.T) {
	if got := Next([]string{"only"}, "only"); got != "only" {
		t.Errorf("got %q, want only", got)
	}
}

func TestNextReturnsZeroForNoEntries(t *testing.T) {
	if got := Next[string](nil, "last"); got != "" {
		t.Errorf("got %q, want the zero value", got)
	}
}
