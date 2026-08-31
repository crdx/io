package web

import (
	"errors"
	"slices"
	"testing"
)

func TestNewBuildsBothReadOnlyConcurrentWebTools(t *testing.T) {
	tools := New(func() bool { return true }, &searchStub{})
	names := make([]string, 0, len(tools))

	for _, offeredTool := range tools {
		names = append(names, offeredTool.Name())
		if !offeredTool.Concurrent() {
			t.Errorf("%s is not concurrent", offeredTool.Name())
		}
		if !offeredTool.ReadOnly() {
			t.Errorf("%s is not read-only", offeredTool.Name())
		}
	}

	if !slices.Equal(names, []string{"web_search", "web_fetch"}) {
		t.Errorf("got tools %v", names)
	}
}

func TestBothToolsConsultAccessWhenTheCallExecutes(t *testing.T) {
	isGranted := false
	for _, offeredTool := range New(func() bool { return isGranted }, &searchStub{}) {
		arguments := `{"query":"weather"}`
		if offeredTool.Name() == "web_fetch" {
			arguments = `{"url":"https://example.com","type":"text"}`
		}

		call, err := offeredTool.Parse(arguments)
		if err != nil {
			t.Fatalf("%s: unexpected parse error: %v", offeredTool.Name(), err)
		}
		if _, err := call.Exec(t.Context()); !errors.Is(err, ErrAccessWithheld) {
			t.Errorf("%s: got %v, want withheld access", offeredTool.Name(), err)
		}
	}
}
