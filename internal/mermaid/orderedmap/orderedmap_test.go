package orderedmap

import "testing"

func TestOrderedMapCoversInsertionReplacementLookupAndIteration(t *testing.T) {
	values := NewOrderedMap[string, int]()
	if values.Front() != nil {
		t.Error("expected an empty map to have no front element")
	}
	if value, ok := values.Get("missing"); ok || value != 0 {
		t.Errorf("missing lookup returned %d, %v", value, ok)
	}

	values.Set("first", 1)
	values.Set("second", 2)
	values.Set("first", 3)
	if value, ok := values.Get("first"); !ok || value != 3 {
		t.Errorf("replacement lookup returned %d, %v", value, ok)
	}

	first := values.Front()
	if first == nil || first.Key != "first" || first.Value != 3 {
		t.Fatalf("unexpected first element: %#v", first)
	}
	second := first.Next()
	if second == nil || second.Key != "second" || second.Value != 2 {
		t.Fatalf("unexpected second element: %#v", second)
	}
	if second.Next() != nil {
		t.Error("expected iteration to end after the second element")
	}
}
