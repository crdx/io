package tool_test

import (
	"context"
	"errors"
	"testing"

	"crdx.org/io/tool"
)

type Params struct {
	City string `json:"city"`
}

func TestOutputStats(t *testing.T) {
	for name, test := range map[string]struct {
		output string
		lines  int64
		bytes  int64
	}{
		"empty":            {output: "", lines: 0, bytes: 0},
		"one line":         {output: "hello", lines: 1, bytes: 5},
		"multiple lines":   {output: "hello\nworld", lines: 2, bytes: 11},
		"trailing newline": {output: "hello\nworld\n", lines: 2, bytes: 12},
	} {
		t.Run(name, func(t *testing.T) {
			got := tool.OutputStats(test.output)
			want := tool.Stats{
				Kind:       tool.StatsOutput,
				Lines:      test.lines,
				Bytes:      test.bytes,
				TotalBytes: test.bytes,
			}
			if got != want {
				t.Errorf("got %#v, want %#v", got, want)
			}
		})
	}
}

func newToolBuilder(t *testing.T) tool.Builder[Params] {
	t.Helper()

	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args Params) (string, string) { return args.City, "" },
	)
}

func newTool(t *testing.T, ran *bool) tool.Tool {
	t.Helper()

	return newToolBuilder(t).Plain(func(_ context.Context, args Params) (string, error) {
		*ran = true
		return "raining in " + args.City, nil
	})
}

func TestParseBindsTheArgumentsToTheCall(t *testing.T) {
	var ran bool

	call, err := newTool(t, &ran).Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if subject := call.Subject(); subject != "London" {
		t.Errorf("expected the bound arguments, got %q", subject)
	}

	if ran {
		t.Error("expected rendering not to run the tool")
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output != "raining in London" {
		t.Errorf("expected the output, got %q", result.Output)
	}

	if !ran {
		t.Error("expected the tool to have run")
	}
}

func TestParseDescribesTheCallOnce(t *testing.T) {
	descriptions := 0
	definedTool := tool.Implement(
		tool.Definition{Name: "weather"},
		func(args Params) (string, string) {
			descriptions++
			return args.City, "today"
		},
	).Plain(func(_ context.Context, _ Params) (string, error) {
		return "", nil
	})

	call, err := definedTool.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject := call.Subject(); subject != "London" {
		t.Errorf("got subject %q, want London", subject)
	}
	if qualifier := call.Qualifier(); qualifier != "today" {
		t.Errorf("got qualifier %q, want today", qualifier)
	}
	if descriptions != 1 {
		t.Errorf("described call %d times, want 1", descriptions)
	}
}

func TestAnOuterEmphasisReplacesAnInnerOne(t *testing.T) {
	subject := newToolBuilder(t).
		Focuses(func(tool.ToolCall) string { return "London" }).
		Syntax("bash").
		Plain(func(_ context.Context, args Params) (string, error) {
			return "raining in " + args.City, nil
		})

	call, err := subject.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if got := call.Emphasis(); got != want {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestSyntaxCanUseDecodedArgumentsAsItsSource(t *testing.T) {
	subject := newToolBuilder(t).
		SyntaxFrom("bash", func(args Params, subject string) string {
			return args.City + "\n" + subject
		}).
		Plain(func(_ context.Context, _ Params) (string, error) { return "", nil })

	call, err := subject.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := call.Emphasis().Source, "London\nLondon"; got != want {
		t.Errorf("got source %q, want %q", got, want)
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

	if subject := call.Subject(); subject != "" {
		t.Errorf("expected nothing rendered, got %q", subject)
	}
}

func TestValidationRunsForAbsentArguments(t *testing.T) {
	validationError := errors.New("city is required")
	validated := false
	subject := newToolBuilder(t).Validate(func(args Params) error {
		validated = true
		if args.City != "" {
			t.Errorf("expected empty arguments, got %#v", args)
		}
		return validationError
	}).Plain(func(context.Context, Params) (string, error) {
		return "", nil
	})

	call, err := subject.Parse("")
	if !errors.Is(err, validationError) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if !validated {
		t.Error("empty arguments bypassed validation")
	}
	if call != nil {
		t.Error("validation failure produced a call")
	}
}

func TestDefineStatsValidatesDecodedArgumentsWhenAsked(t *testing.T) {
	validationError := errors.New("London is unavailable")
	rendered := false
	executed := false

	subject := tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(_ Params) (string, string) {
			rendered = true
			return "", ""
		},
	).Validate(func(args Params) error {
		if args.City != "London" {
			t.Fatalf("expected decoded arguments, got %#v", args)
		}
		return validationError
	}).Stats(func(_ context.Context, _ Params) (string, tool.Stats, error) {
		executed = true
		return "", tool.Stats{}, nil
	})

	call, err := subject.Parse(`{"city":"London"}`)
	if !errors.Is(err, validationError) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if call != nil {
		t.Error("expected validation to prevent call construction")
	}
	if rendered || executed {
		t.Error("expected validation not to render or execute the call")
	}
}

func TestDefineStatsDoesNotRequireValidation(t *testing.T) {
	subject := tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args Params) (string, string) { return args.City, "" },
	).Stats(func(_ context.Context, args Params) (string, tool.Stats, error) {
		return args.City, tool.Stats{}, nil
	})

	call, err := subject.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject := call.Subject(); subject != "London" {
		t.Errorf("expected the bound arguments, got %q", subject)
	}
}
