package truncate_test

import (
	"os"
	"strings"
	"testing"

	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
)

type Args struct {
	Size int `json:"size"`
}

func newTool(t *testing.T) tool.Tool {
	t.Helper()

	return tool.Define(
		"generate",
		"generate output",
		tool.Schema{tool.Integer("size", "how many lines to generate")},
		func(args Args) string { return "generate" },
		func(args Args) (string, error) {
			return strings.Repeat("a line of text\n", args.Size), nil
		},
	)
}

func exec(t *testing.T, subject tool.Tool, arguments string) string {
	t.Helper()

	call, err := subject.Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call.Exec()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return output
}

func TestOutputThatFitsIsLeftAlone(t *testing.T) {
	output := truncate.Output("hello\n")

	if output != "hello\n" {
		t.Errorf("expected the output untouched, got %q", output)
	}
}

func TestOutputTooBigIsCutAndSaved(t *testing.T) {
	whole := strings.Repeat("a line of text\n", 4000)

	output := truncate.Output(whole)

	if len(output) > truncate.Limit+300 {
		t.Errorf("expected the output to be capped, got %d bytes", len(output))
	}

	if !strings.HasSuffix(strings.SplitN(output, "\n\n[", 2)[0], "a line of text") {
		t.Error("expected the cut to fall on a line boundary")
	}

	path := strings.TrimSuffix(strings.Split(output, "the whole of it is in ")[1], "]\n")
	path = strings.TrimSuffix(strings.TrimSpace(path), "]")

	saved, err := os.ReadFile(path) //nolint:gosec // the path the notice named
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = os.Remove(path) })

	if string(saved) != whole {
		t.Errorf("expected the whole output to be saved, got %d of %d bytes", len(saved), len(whole))
	}
}

func TestAWrappedToolIsCapped(t *testing.T) {
	subject := truncate.Tool(newTool(t))

	if subject.Name() != "generate" {
		t.Errorf("expected the name to survive, got %q", subject.Name())
	}

	small := exec(t, subject, `{"size":2}`)
	if small != "a line of text\na line of text\n" {
		t.Errorf("expected a small reply untouched, got %q", small)
	}

	big := exec(t, subject, `{"size":4000}`)
	if !strings.Contains(big, "truncated at") {
		t.Error("expected an oversized reply to be cut")
	}
}

func TestAnUnwrappedToolIsNotCapped(t *testing.T) {
	output := exec(t, newTool(t), `{"size":4000}`)

	if strings.Contains(output, "truncated at") {
		t.Error("expected an unwrapped tool to hand back everything")
	}
}

func TestToolsWrapsEveryTool(t *testing.T) {
	wrapped := truncate.Tools([]tool.Tool{newTool(t), newTool(t)})

	if len(wrapped) != 2 {
		t.Fatalf("expected both tools back, got %d", len(wrapped))
	}

	for _, subject := range wrapped {
		if !strings.Contains(exec(t, subject, `{"size":4000}`), "truncated at") {
			t.Error("expected every tool to be capped")
		}
	}
}
