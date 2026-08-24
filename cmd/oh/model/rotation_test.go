package model

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRoundRobinSelectionsResolveEveryEntry(t *testing.T) {
	useCachedModels(t)

	selections, err := ParseRoundRobin(modelCachePath(), []string{
		"opencode/deepseek@hi",
		"anth/opus-5@max",
	})
	if err != nil {
		t.Fatal(err)
	}

	wanted := []string{
		"opencode-go/deepseek-v4-pro@high",
		"anthropic/claude-opus-5@max",
	}
	for i, selection := range selections {
		if selection.String() != wanted[i] {
			t.Errorf("selection %d is %s, want %s", i, selection, wanted[i])
		}
	}
}

func TestRoundRobinSelectionsRejectCanonicalDuplicates(t *testing.T) {
	useCachedModels(t)

	_, err := ParseRoundRobin(modelCachePath(), []string{
		"opencode-go/deepseek-v4-pro@high",
		"opencode/deepseek@hi",
	})
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected a duplicate error, got %v", err)
	}
}

func TestOneRoundRobinSelectionNeedsNoState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	selection := Selection{Provider: CodexProvider, Model: "only", Effort: "high"}

	selected, err := ReserveRoundRobin(path, []Selection{selection})
	if err != nil {
		t.Fatal(err)
	}
	if selected != selection {
		t.Errorf("got %s, want %s", selected, selection)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no state, got %v", err)
	}
}

func TestConcurrentRoundRobinReservationsShareTheStateLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "round-robin.json")
	selections := []Selection{
		{Provider: CodexProvider, Model: "first", Effort: "high"},
		{Provider: AnthropicProvider, Model: "second", Effort: "high"},
	}
	const calls = 20

	selected := make(chan Selection, calls)
	var wait sync.WaitGroup
	for range calls {
		wait.Go(func() {
			selection, err := ReserveRoundRobin(path, selections)
			if err != nil {
				t.Error(err)
				return
			}
			selected <- selection
		})
	}
	wait.Wait()
	close(selected)

	counts := map[Selection]int{}
	for selection := range selected {
		counts[selection]++
	}
	for _, selection := range selections {
		if counts[selection] != calls/len(selections) {
			t.Errorf("selected %s %d times", selection, counts[selection])
		}
	}
}

func TestRoundRobinSelectionsValidateEntriesThatAreNotFirst(t *testing.T) {
	useCachedModels(t)

	_, err := ParseRoundRobin(modelCachePath(), []string{
		"sol@high",
		"nothing-like-this@high",
	})
	if err == nil || !strings.Contains(err.Error(), "nothing-like-this") {
		t.Fatalf("expected the invalid selection to be named, got %v", err)
	}
}
