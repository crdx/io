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

func text(content string) agent.Event {
	return agent.Event{Kind: agent.ModelMessage, Text: content}
}

func renderedEvents(events iter.Seq2[agent.Event, error]) ([]string, error) {
	var eventStrings []string

	for event, err := range events {
		if err != nil {
			return eventStrings, err
		}

		eventStrings = append(eventStrings, fmt.Sprintf("%s:%s%s", event.Kind, event.Name, event.Text))
	}

	return eventStrings, nil
}

func TestCoalesceHoldsTextUntilSomethingElseHappens(t *testing.T) {
	coalescedEvents := agent.Coalesce(stream(
		agent.Event{Kind: agent.ModelReasoning, Text: "Need to "},
		agent.Event{Kind: agent.ModelReasoning, Text: "look. "},
		text("Let me "),
		text("look. "),
		agent.Event{Kind: agent.ToolCallRequest, Name: "weather"},
		agent.Event{Kind: agent.ToolCallResult, Name: "weather", Text: "raining"},
		text("It is "),
		text("raining."),
	))

	eventStrings, err := renderedEvents(coalescedEvents)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedEvents := []string{
		fmt.Sprintf("%s:Need to look. ", agent.ModelReasoning),
		fmt.Sprintf("%s:Let me look. ", agent.ModelMessage),
		fmt.Sprintf("%s:weather", agent.ToolCallRequest),
		fmt.Sprintf("%s:weatherraining", agent.ToolCallResult),
		fmt.Sprintf("%s:It is raining.", agent.ModelMessage),
	}

	if !slices.Equal(eventStrings, expectedEvents) {
		t.Errorf("expected %v, got %v", expectedEvents, eventStrings)
	}
}

func TestCoalesceLetsHeldTextGoBeforeAnError(t *testing.T) {
	failure := errors.New("model overloaded")

	coalescedEvents := agent.Coalesce(func(yield func(agent.Event, error) bool) {
		if yield(text("It is raining "), nil) {
			yield(agent.Event{}, failure)
		}
	})

	eventStrings, err := renderedEvents(coalescedEvents)
	if !errors.Is(err, failure) {
		t.Fatalf("expected the failure, got %v", err)
	}

	if !slices.Equal(eventStrings, []string{fmt.Sprintf("%s:It is raining ", agent.ModelMessage)}) {
		t.Errorf("expected what was said to survive the error, got %v", eventStrings)
	}
}

func TestCoalesceStopsWhenTheCallerDoes(t *testing.T) {
	var readEvents int

	countedStream := func(yield func(agent.Event, error) bool) {
		callEvent := agent.Event{Kind: agent.ToolCallRequest, Name: "weather"}

		for _, event := range []agent.Event{text("It is raining "), callEvent, callEvent} {
			readEvents++

			if !yield(event, nil) {
				return
			}
		}
	}

	var eventStrings int

	for range agent.Coalesce(countedStream) {
		eventStrings++
		break
	}

	if eventStrings != 1 {
		t.Errorf("expected one event, got %d", eventStrings)
	}

	if readEvents != 2 {
		t.Errorf("expected the text and the call that let it go to be all that was read, "+
			"got %d events", readEvents)
	}
}
