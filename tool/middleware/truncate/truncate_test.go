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
	Size int `json:"size"` // how much output to make
}

func newTool(t *testing.T) tool.Tool {
	t.Helper()

	return tool.Define(
		"generate",
		"generate output",
		tool.Schema{tool.Integer("size", "how many lines to generate")},
		func(args Args) (string, string) { return "generate", "" },
		func(_ context.Context, args Args) (string, error) {
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

	output, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return output
}

func TestStatisticsPassThroughTheOutputCap(t *testing.T) {
	subject := tool.DefineMeasured(
		"measured",
		"measure something",
		tool.Schema{},
		func(Args) (string, string) { return "measured", "" },
		func(context.Context, Args) (string, tool.Statistics, error) {
			return "done", tool.Statistics{Kind: tool.StatsRead, Lines: 3, Bytes: 12}, nil
		},
	)

	call, err := truncate.Tool(subject).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); err != nil {
		t.Fatal(err)
	}

	stats, ok := tool.Stats(call)
	if !ok || stats.Lines != 3 || stats.Bytes != 12 {
		t.Errorf("got %+v and measured=%v", stats, ok)
	}
}

func TestTruncatedStatisticsReportReturnedAndTotalOutput(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	whole := strings.Repeat("a line of text\n", 4000)
	subject := tool.DefineMeasured(
		"measured",
		"measure something",
		tool.Schema{},
		func(Args) (string, string) { return "measured", "" },
		func(context.Context, Args) (string, tool.Statistics, error) {
			return whole, tool.Statistics{Kind: tool.StatsResources}, nil
		},
	)

	call, err := truncate.Tool(subject).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); err != nil {
		t.Fatal(err)
	}

	stats, ok := tool.Stats(call)
	if !ok || stats.Bytes <= 0 || stats.Bytes > truncate.Limit || stats.TotalBytes != int64(len(whole)) || !stats.Truncated {
		t.Errorf("expected returned and total output statistics, got %+v and measured=%v", stats, ok)
	}
}

func TestAnAttachedImagePassesThroughTheOutputCap(t *testing.T) {
	subject := tool.DefineMeasuredWithImage(
		"image",
		"return an image",
		tool.Schema{},
		func(Args) (string, string) { return "image", "" },
		func(context.Context, Args) (string, tool.Image, tool.Statistics, error) {
			return "image/png image", tool.Image{MediaType: "image/png", Data: []byte{1}}, tool.Statistics{}, nil
		},
	)

	call, err := truncate.Tool(subject).Parse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); err != nil {
		t.Fatal(err)
	}

	image, ok := tool.AttachedImage(call)
	if !ok || image.MediaType != "image/png" || len(image.Data) != 1 {
		t.Errorf("expected the image to pass through, got %+v and attached=%v", image, ok)
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

func TestAWrappedToolKeepsItsSyntax(t *testing.T) {
	subject := tool.Syntax(newTool(t), "bash")
	call, err := truncate.Tool(subject).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	syntaxCall, ok := call.(tool.SyntaxCall)
	if !ok || syntaxCall.Syntax() != "bash" {
		t.Errorf("expected the syntax to survive, got %T", call)
	}
}

func TestAWrappedToolKeepsItsFocusedRendering(t *testing.T) {
	subject := tool.Focus(newTool(t), func(tool.Call) string { return "generate" })
	call, err := truncate.Tool(subject).Parse(`{"size":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	focusedCall, ok := call.(tool.FocusedCall)
	if !ok || focusedCall.Focus() != "generate" {
		t.Errorf("expected the focus to survive, got %T", call)
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
	wrappedTools := truncate.Tools([]tool.Tool{newTool(t), newTool(t)})

	if len(wrappedTools) != 2 {
		t.Fatalf("expected both tools back, got %d", len(wrappedTools))
	}

	for _, subject := range wrappedTools {
		if !strings.Contains(exec(t, subject, `{"size":4000}`), "truncated at") {
			t.Error("expected every tool to be capped")
		}
	}
}
