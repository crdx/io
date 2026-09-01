package roundrobin

import "testing"

func TestNextIndexStartsAtTheBeginningAndWraps(t *testing.T) {
	entries := []string{"first", "second"}

	if got := NextIndex(entries, "", -1); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
	if got := NextIndex(entries, "first", 0); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
	if got := NextIndex(entries, "second", 1); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestNextIndexStartsOverWhenTheLastEntryWasRemoved(t *testing.T) {
	if got := NextIndex([]string{"second", "third"}, "first", 0); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestNextIndexReturnsTheOnlyEntry(t *testing.T) {
	if got := NextIndex([]string{"only"}, "only", 0); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestNextIndexReturnsMinusOneForNoEntries(t *testing.T) {
	if got := NextIndex(nil, "last", 0); got != -1 {
		t.Errorf("got %d, want -1", got)
	}
}

func TestNextIndexWalksRepeatedEntriesOneAtATime(t *testing.T) {
	entries := []string{"first", "first", "second"}

	if got := NextIndex(entries, "first", 0); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
	if got := NextIndex(entries, "first", 1); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
	if got := NextIndex(entries, "second", 2); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestNextIndexFallsBackToTheFirstMatchWhenTheIndexIsStale(t *testing.T) {
	entries := []string{"first", "second", "third"}

	if got := NextIndex(entries, "second", 0); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
	if got := NextIndex(entries, "second", 99); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}
