package tool_test

import (
	"context"
	"testing"

	"crdx.org/io/tool"
)

type Params struct {
	City string `json:"city"` // the city to report
}

func newTool(t *testing.T, ran *bool) tool.Tool {
	t.Helper()

	return tool.Define(
		"weather",
		"report weather in a city",
		tool.Schema{tool.String("city", "the city to look up")},
		func(args Params) (string, string) { return args.City, "" },
		func(_ context.Context, args Params) (string, error) {
			*ran = true
			return "raining in " + args.City, nil
		},
	)
}

func TestParseBindsTheArgumentsToTheCall(t *testing.T) {
	var ran bool

	call, err := newTool(t, &ran).Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if renderedCall := call.Render(); renderedCall != "London" {
		t.Errorf("expected the bound arguments, got %q", renderedCall)
	}

	if ran {
		t.Error("expected rendering not to run the tool")
	}

	output, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "raining in London" {
		t.Errorf("expected the output, got %q", output)
	}

	if !ran {
		t.Error("expected the tool to have run")
	}
}

func TestParseRefusesArgumentsItCannotRead(t *testing.T) {
	var ran bool

	call, err := newTool(t, &ran).Parse(`{"city":`)
	if err == nil {
		t.Fatal("expected malformed arguments to be refused")
	}

	if call != nil {
		t.Error("expected no call to be handed back")
	}

	if ran {
		t.Error("expected the tool never to run")
	}
}

func TestParseTakesAbsentArgumentsAsEmpty(t *testing.T) {
	var ran bool

	call, err := newTool(t, &ran).Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if renderedCall := call.Render(); renderedCall != "" {
		t.Errorf("expected nothing rendered, got %q", renderedCall)
	}
}
