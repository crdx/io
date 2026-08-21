package agent_test

import (
	"reflect"
	"testing"

	"crdx.org/io/agent"
)

func TestCoalescerCanBeDrivenIncrementally(t *testing.T) {
	var held agent.Coalescer
	var got []agent.Event

	got = append(got, held.Add(agent.Event{Kind: agent.ModelMessage, Text: "one "})...)
	got = append(got, held.Add(agent.Event{Kind: agent.ModelMessage, Text: "two"})...)
	got = append(got, held.Add(agent.Event{Kind: agent.ToolCallRequest, Name: "read"})...)
	got = append(got, held.Flush()...)

	want := []agent.Event{
		{Kind: agent.ModelMessage, Text: "one two"},
		{Kind: agent.ToolCallRequest, Name: "read"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
