package agent_test

import (
	"errors"
	"fmt"
	"iter"
	"slices"
	"testing"

	"crdx.org/io/agent"
)

func stream(events ...agent.Event) iter.Seq2[agent.Event, error] {
	return func(yield func(agent.Event, error) bool) {
		for _, event := range events {
			if !yield(event, nil) {
				return
			}
		}
	}
}

func text(said string) agent.Event {
	return agent.Event{Kind: agent.Text, Value: said}
}

func rendered(events iter.Seq2[agent.Event, error]) ([]string, error) {
	var seen []string

	for event, err := range events {
		if err != nil {
			return seen, err
		}

		seen = append(seen, fmt.Sprintf("%d:%s%s", event.Kind, event.Name, event.Value))
	}

	return seen, nil
}

func TestCoalesceHoldsTextUntilSomethingElseHappens(t *testing.T) {
	held := agent.Coalesce(stream(
		text("Let me "),
		text("look. "),
		agent.Event{Kind: agent.Call, Name: "weather"},
		agent.Event{Kind: agent.Result, Name: "weather", Value: "raining"},
		text("It is "),
		text("raining."),
	))

	seen, err := rendered(held)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		fmt.Sprintf("%d:Let me look. ", agent.Text),
		fmt.Sprintf("%d:weather", agent.Call),
		fmt.Sprintf("%d:weatherraining", agent.Result),
		fmt.Sprintf("%d:It is raining.", agent.Text),
	}

	if !slices.Equal(seen, expected) {
		t.Errorf("expected %v, got %v", expected, seen)
	}
}

func TestCoalesceLetsHeldTextGoBeforeAnError(t *testing.T) {
	failure := errors.New("model overloaded")

	held := agent.Coalesce(func(yield func(agent.Event, error) bool) {
		if yield(text("It is raining "), nil) {
			yield(agent.Event{}, failure)
		}
	})

	seen, err := rendered(held)
	if !errors.Is(err, failure) {
		t.Fatalf("expected the failure, got %v", err)
	}

	if !slices.Equal(seen, []string{fmt.Sprintf("%d:It is raining ", agent.Text)}) {
		t.Errorf("expected what was said to survive the error, got %v", seen)
	}
}

func TestCoalesceStopsWhenTheCallerDoes(t *testing.T) {
	var read int

	counted := func(yield func(agent.Event, error) bool) {
		asked := agent.Event{Kind: agent.Call, Name: "weather"}

		for _, event := range []agent.Event{text("It is raining "), asked, asked} {
			read++

			if !yield(event, nil) {
				return
			}
		}
	}

	var seen int

	for range agent.Coalesce(counted) {
		seen++
		break
	}

	if seen != 1 {
		t.Errorf("expected one event, got %d", seen)
	}

	if read != 2 {
		t.Errorf("expected the text and the call that let it go to be all that was read, "+
			"got %d events", read)
	}
}
