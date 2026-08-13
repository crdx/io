package tool_test

import (
	"testing"

	"crdx.org/io/tool"
)

type Params struct {
	City string `json:"city"`
}

func newTool(t *testing.T, ran *bool) tool.Tool {
	t.Helper()

	return tool.Define(
		"weather",
		"report weather in a city",
		tool.Schema{tool.String("city", "the city to look up")},
		func(args Params) string { return args.City },
		func(args Params) (string, error) {
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

	if rendered := call.Render(); rendered != "London" {
		t.Errorf("expected the bound arguments, got %q", rendered)
	}

	if ran {
		t.Error("expected rendering not to run the tool")
	}

	output, err := call.Exec()
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

// A call that cannot be understood is refused at the boundary, which is the whole point of decoding
// once: nothing is rendered and nothing is run.
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

// Absent arguments are the zero value, which is what a call carrying none means.
func TestParseTakesAbsentArgumentsAsEmpty(t *testing.T) {
	var ran bool

	call, err := newTool(t, &ran).Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rendered := call.Render(); rendered != "" {
		t.Errorf("expected nothing rendered, got %q", rendered)
	}
}
