package state

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

type testState struct {
	Count int `json:"count"`
}

func TestUpdateCarriesStateForward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	for range 3 {
		if err := Update(path, func(state *testState) error {
			state.Count++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := Update(path, func(state *testState) error {
		if state.Count != 3 {
			t.Errorf("got count %d, want 3", state.Count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUpdatesShareTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	const updates = 20

	var wait sync.WaitGroup
	for range updates {
		wait.Go(func() {
			if err := Update(path, func(state *testState) error {
				state.Count++
				return nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wait.Wait()

	if err := Update(path, func(state *testState) error {
		if state.Count != updates {
			t.Errorf("got count %d, want %d", state.Count, updates)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFailedUpdateDoesNotReplaceState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := Update(path, func(state *testState) error {
		state.Count = 4
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	failure := errors.New("stop")
	if err := Update(path, func(state *testState) error {
		state.Count = 99
		return failure
	}); !errors.Is(err, failure) {
		t.Fatalf("got %v, want %v", err, failure)
	}

	if err := Update(path, func(state *testState) error {
		if state.Count != 4 {
			t.Errorf("got count %d, want 4", state.Count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
