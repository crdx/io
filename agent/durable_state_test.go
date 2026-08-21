package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/tool"
)

func statefulTool(restored *json.RawMessage) tool.Tool {
	definedTool := tool.Implement(
		tool.Definition{
			Name:        "noop",
			Description: "",
			Schema:      tool.Schema{},
		},
		func(struct{}) (string, string) { return "", "" },
	).Run(func(context.Context, struct{}) (tool.Result, error) {
		return tool.Result{
			Output: "done",
			State:  json.RawMessage(`{"answer":42}`),
		}, nil
	})

	return tool.State(definedTool, "test_state", func(state json.RawMessage) error {
		*restored = append((*restored)[:0], state...)
		return nil
	})
}

func TestASuccessfulCallEmitsItsDurableStateBeforeItsResult(t *testing.T) {
	provider := &callProvider{}
	var restored json.RawMessage
	assistant := agent.New("", provider, []tool.Tool{statefulTool(&restored)})
	var events []agent.Event

	for event, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}

	var relevant []agent.Kind
	var state json.RawMessage
	var stateKey string
	for _, event := range events {
		if event.Kind == agent.ToolCallResult || event.Kind == agent.StateChange {
			relevant = append(relevant, event.Kind)
		}
		if event.Kind == agent.StateChange {
			state = event.State
			stateKey = event.Name
		}
	}

	wantKinds := []agent.Kind{agent.StateChange, agent.ToolCallResult, agent.StateChange, agent.ToolCallResult}
	if !reflect.DeepEqual(relevant, wantKinds) {
		t.Errorf("got event kinds %v, want %v", relevant, wantKinds)
	}
	if stateKey != "test_state" || string(state) != `{"answer":42}` {
		t.Errorf("got %s state %s", stateKey, state)
	}
	if string(restored) != `{"answer":42}` {
		t.Errorf("got live state %s", restored)
	}
}

func TestAFailedCallDoesNotEmitDurableState(t *testing.T) {
	provider := &callProvider{}
	failedTool := tool.Implement(
		tool.Definition{Name: "noop", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).Run(func(context.Context, struct{}) (tool.Result, error) {
		return tool.Result{State: json.RawMessage(`{"answer":42}`)}, errors.New("failed")
	})
	assistant := agent.New("", provider, []tool.Tool{failedTool})

	for event, err := range assistant.Stream(t.Context(), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind == agent.StateChange {
			t.Error("a failed call emitted durable state")
		}
	}
}

func TestStateCanBeRestoredIntoANewAgent(t *testing.T) {
	provider := &callProvider{}
	var restored json.RawMessage
	assistant := agent.New("", provider, []tool.Tool{statefulTool(&restored)})
	events := []agent.Event{{
		Kind:  agent.StateChange,
		Name:  "test_state",
		State: json.RawMessage(`{"answer":42}`),
	}}

	if err := assistant.RestoreState(events); err != nil {
		t.Fatal(err)
	}
	if string(restored) != `{"answer":42}` {
		t.Errorf("got restored state %s", restored)
	}
}
