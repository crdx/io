package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/tool"
)

type stateProvider struct {
	items []json.RawMessage
}

func (*stateProvider) Configure(string, []tool.Definition) {}
func (*stateProvider) AddUserMessage(string)               {}
func (*stateProvider) AddToolResults([]agent.ToolResult)   {}
func (*stateProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}
func (p *stateProvider) Dump() []json.RawMessage      { return slices.Clone(p.items) }
func (p *stateProvider) Load(items []json.RawMessage) { p.items = slices.Clone(items) }

func TestAgentCarriesProviderState(t *testing.T) {
	provider := &stateProvider{}
	assistant := agent.New("", provider, nil)
	want := []json.RawMessage{json.RawMessage(`{"type":"message"}`)}

	if err := assistant.Load(want); err != nil {
		t.Fatal(err)
	}
	got, err := assistant.Dump()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(got, want, func(a, b json.RawMessage) bool { return string(a) == string(b) }) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAgentRejectsStateThatIsNotAppendOnly(t *testing.T) {
	provider := &stateProvider{items: []json.RawMessage{json.RawMessage(`{"one":1}`)}}
	assistant := agent.New("", provider, nil)
	if _, err := assistant.Dump(); err != nil {
		t.Fatal(err)
	}

	provider.items[0] = json.RawMessage(`{"two":2}`)
	if _, err := assistant.Dump(); !errors.Is(err, agent.ErrStateReplaced) {
		t.Errorf("expected ErrStateReplaced, got %v", err)
	}
}

func TestAgentReportsAProviderWithoutState(t *testing.T) {
	assistant := agent.New("", &callProvider{}, nil)
	if _, err := assistant.Dump(); !errors.Is(err, agent.ErrNoState) {
		t.Errorf("expected ErrNoState, got %v", err)
	}
	if err := assistant.Load(nil); !errors.Is(err, agent.ErrNoState) {
		t.Errorf("expected ErrNoState, got %v", err)
	}
}
