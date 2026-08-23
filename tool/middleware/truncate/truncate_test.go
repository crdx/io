package truncate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/tool"
	"crdx.org/io/tool/middleware/truncate"
)

type Args struct {
	Size int `json:"size"`
}

func newToolBuilder(t *testing.T) tool.Builder[Args] {
	t.Helper()

	return tool.Implement(
		tool.Definition{
			Name:        "generate",
			Description: "generate output",
			Schema:      tool.Schema{tool.Integer("size", "how many lines to generate")},
		},
		func(args Args) (string, string) { return "generate", "" },
	)
}

func buildTool(builder tool.Builder[Args]) tool.Tool {
	return builder.Plain(func(_ context.Context, args Args) (string, error) {
		return strings.Repeat("a line of text\n", args.Size), nil
	})
}

func exec(t *testing.T, subject tool.Tool, arguments string) string {
	t.Helper()

	call, err := subject.Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return result.Output
}

func TestStatisticsPassThroughTheOutputCap(t *testing.T) {
	subject := tool.Implement(
		tool.Definition{
			Name:        "measured",
			Description: "measure something",
			Schema:      tool.Schema{},
		},
		func(Args) (string, string) { return "measured", "" },
	).Stats(func(context.Context, Args) (string, tool.Stats, error) {
		return "done", tool.Stats{Kind: tool.StatsRead, Lines: 3, Bytes: 12}, nil
	})

	call, err := truncate.Tool(subject).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.Lines != 3 || result.Stats.Bytes != 12 {
		t.Errorf("got %+v", result.Stats)
	}
}

func TestTruncatedStatisticsReportReturnedAndTotalOutput(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	whole := strings.Repeat("a line of text\n", 4000)
	subject := tool.Implement(
		tool.Definition{
			Name:        "measured",
			Description: "measure something",
			Schema:      tool.Schema{},
		},
		func(Args) (string, string) { return "measured", "" },
	).Stats(func(context.Context, Args) (string, tool.Stats, error) {
		return whole, tool.Stats{Kind: tool.StatsResources}, nil
	})

	call, err := truncate.Tool(subject).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	statistics := result.Stats
	if statistics.Bytes <= 0 || statistics.Bytes > truncate.Limit || statistics.TotalBytes != int64(len(whole)) || !statistics.Truncated {
		t.Errorf("expected returned and total output statistics, got %+v", statistics)
	}
}

func TestAnAttachedImagePassesThroughTheOutputCap(t *testing.T) {
	subject := tool.Implement(
		tool.Definition{
			Name:        "image",
			Description: "return an image",
			Schema:      tool.Schema{},
		},
		func(Args) (string, string) { return "image", "" },
	).StatsWithImage(func(context.Context, Args) (string, tool.Image, tool.Stats, error) {
		return "image/png image", tool.Image{MediaType: "image/png", Data: []byte{1}}, tool.Stats{}, nil
	})

	call, err := truncate.Tool(subject).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if result.Image.MediaType != "image/png" || len(result.Image.Data) != 1 {
		t.Errorf("expected the image to pass through, got %+v", result.Image)
	}
}

func TestOutputThatFitsIsLeftAlone(t *testing.T) {
	output := truncate.Output("hello\n")

	if output != "hello\n" {
		t.Errorf("expected the output untouched, got %q", output)
	}
}

func TestOutputTooBigIsCutAndSaved(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	whole := strings.Repeat("a line of text\n", 4000)

	output := truncate.Output(whole)

	if len(output) > truncate.Limit+300 {
		t.Errorf("expected the output to be capped, got %d bytes", len(output))
	}

	if !strings.HasSuffix(strings.SplitN(output, "\n\n[", 2)[0], "a line of text") {
		t.Error("expected the cut to fall on a line boundary")
	}

	saved, err := filepath.Glob(filepath.Join(os.TempDir(), "io-output-*.txt"))
	if err != nil || len(saved) != 1 {
		t.Fatalf("expected the whole of it saved once, got %v and %v", saved, err)
	}

	if !strings.Contains(output, "truncated at 32K of 58.6K") {
		t.Errorf("expected compact byte sizes in the notice, got %q", output)
	}
	if !strings.Contains(output, saved[0]) {
		t.Errorf("expected the notice to name the file it saved, got %q", output)
	}

	savedOutput, err := os.ReadFile(saved[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(savedOutput) != whole {
		t.Errorf("expected the whole output to be saved, got %d of %d bytes", len(savedOutput), len(whole))
	}
}

func TestAWrappedToolKeepsItsSyntaxHighlighting(t *testing.T) {
	subject := buildTool(newToolBuilder(t).Syntax("bash"))
	call, err := truncate.Tool(subject).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if call.Emphasis() != want {
		t.Errorf("expected the emphasis to survive, got %T", call)
	}
}

func TestAWrappedToolKeepsItsFocusedRendering(t *testing.T) {
	subject := buildTool(newToolBuilder(t).Focuses(func(tool.ToolCall) string { return "generate" }))
	call, err := truncate.Tool(subject).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisFocus, Value: "generate"}
	if call.Emphasis() != want {
		t.Errorf("expected the focus to survive, got %T", call)
	}
}

func TestAWrappedToolIsCapped(t *testing.T) {
	subject := truncate.Tool(buildTool(newToolBuilder(t)))

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
	output := exec(t, buildTool(newToolBuilder(t)), `{"size":4000}`)

	if strings.Contains(output, "truncated at") {
		t.Error("expected an unwrapped tool to hand back everything")
	}
}

func TestToolsWrapsEveryTool(t *testing.T) {
	wrappedTools := truncate.Tools([]tool.Tool{
		buildTool(newToolBuilder(t)),
		buildTool(newToolBuilder(t)),
	})

	if len(wrappedTools) != 2 {
		t.Fatalf("expected both tools back, got %d", len(wrappedTools))
	}

	for _, subject := range wrappedTools {
		if !strings.Contains(exec(t, subject, `{"size":4000}`), "truncated at") {
			t.Error("expected every tool to be capped")
		}
	}
}
