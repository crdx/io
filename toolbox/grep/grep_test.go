package grep_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
	"crdx.org/io/toolbox/grep"
)

func testRoot(t *testing.T, files map[string]string) *file.Root {
	t.Helper()

	directory := t.TempDir()

	for path, content := range files {
		full := filepath.Join(directory, path)

		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, allowAll)
}

func exec(t *testing.T, root *file.Root, arguments string) (string, error) {
	t.Helper()

	output, _, err := execWithStats(t, root, arguments)
	return output, err
}

func execWithStats(
	t *testing.T,
	root *file.Root,
	arguments string,
) (string, tool.Statistics, error) {
	t.Helper()

	call, err := grep.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call.Exec(t.Context())
	stats, _ := tool.Stats(call)
	return output, stats, err
}

func TestAMatchIsReportedWithItsPathAndLine(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	output, err := exec(t, root, `{"pattern":"func main"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "main.go:3:func main() {}" {
		t.Errorf("expected the path, line and text, got %q", output)
	}
}

func TestTheNumberOfMatchingLinesIsReported(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\ngoodbye\nhello\n"})

	output, stats, err := execWithStats(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stats.Kind != tool.StatsSearch || stats.Lines != 2 || stats.Truncated {
		t.Errorf("expected two uncapped matching lines, got %+v", stats)
	}
	if stats.Bytes != int64(len(output)) || stats.TotalBytes != stats.Bytes {
		t.Errorf("expected %d returned and total bytes, got %+v", len(output), stats)
	}
}

func TestAGlobNarrowsWhatIsSearched(t *testing.T) {
	root := testRoot(t, map[string]string{
		"main.go":   "hello\n",
		"notes.txt": "hello\n",
	})

	output, err := exec(t, root, `{"pattern":"hello","glob":"**/*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(output, "notes.txt") {
		t.Errorf("expected only the Go file, got %q", output)
	}
}

func TestIgnoredFilesAreNotSearched(t *testing.T) {
	root := testRoot(t, map[string]string{
		".gitignore":          "ignored/\n",
		"kept.txt":            "hello\n",
		"ignored/ignored.txt": "hello\n",
	})

	output, err := exec(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "kept.txt:1:hello" {
		t.Errorf("expected ignored files to be skipped, got %q", output)
	}
}

func TestAPatternThatWillNotCompileIsRefused(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})

	_, err := exec(t, root, `{"pattern":"("}`)
	if err == nil {
		t.Fatal("expected an invalid pattern to be refused")
	}

	if !strings.Contains(err.Error(), "invalid pattern") {
		t.Errorf("expected the refusal to name the pattern, got %q", err)
	}
}

func TestASearchThatFindsNothingSaysSo(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})

	output, err := exec(t, root, `{"pattern":"goodbye"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "(no matches)" {
		t.Errorf("expected no matches to say so, got %q", output)
	}
}

func TestMatchCountDoesNotCapSmallResults(t *testing.T) {
	const matchCount = 150
	root := testRoot(t, map[string]string{
		"big.txt": strings.Repeat("hello\n", matchCount),
	})

	output, stats, err := execWithStats(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stats.Lines != matchCount || stats.Truncated {
		t.Errorf("expected all %d small matches, got %+v", matchCount, stats)
	}
	if strings.Contains(output, "narrow the search") {
		t.Errorf("expected the complete result, got %q", output)
	}
}

func TestHittingTheByteCapIsSaidOutLoud(t *testing.T) {
	root := testRoot(t, map[string]string{
		"big.txt": strings.Repeat(strings.Repeat("x", 100)+"\n", 500),
	})

	output, stats, err := execWithStats(t, root, `{"pattern":"x"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "matching output exceeded 16K") {
		t.Errorf("expected the byte cap to be reported, got the last of %q", output[len(output)-100:])
	}
	if stats.Lines <= 0 || !stats.Truncated {
		t.Errorf("expected capped matching lines, got %+v", stats)
	}
	if stats.Bytes <= 0 || stats.Bytes > util.MaxSearchBytes || stats.TotalBytes != 0 {
		t.Errorf("expected returned bytes and an unknown total, got %+v", stats)
	}
}

func TestACancelledContextStopsTheSearch(t *testing.T) {
	root := testRoot(t, map[string]string{"main.go": "hello\n"})

	bin := t.TempDir()
	//nolint:gosec // an executable test fixture
	if err := os.WriteFile(
		filepath.Join(bin, "rg"),
		[]byte("#!/bin/sh\nexec /bin/sleep 60\n"),
		0o700,
	); err != nil {
		t.Fatalf("could not write fake rg: %v", err)
	}
	t.Setenv("PATH", bin)

	call, err := grep.New(root).Parse(`{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)

	startedAt := time.Now()
	_, err = call.Exec(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context cancellation, got %v", err)
	}
	if took := time.Since(startedAt); took > 2*time.Second {
		t.Errorf("expected the search to stop promptly, took %s", took)
	}
}

func TestCallHighlightsItsPatternAsRegexpSyntax(t *testing.T) {
	root := testRoot(t, nil)
	call, err := grep.New(root).Parse(`{"pattern":"foo|bar","path":"internal/file.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	highlightedCall, ok := call.(tool.HighlightedCall)
	want := tool.Highlight{Kind: tool.HighlightSyntax, Value: "regexp"}
	if !ok || highlightedCall.Highlight() != want {
		t.Errorf("expected regexp highlighting, got %T", call)
	}
}

func TestRenderSaysNothingOfTheWorkingDirectory(t *testing.T) {
	renderedPattern, detail := grep.Render(grep.Args{Pattern: "hello", Path: "."})
	if renderedPattern != "hello" || detail != "" {
		t.Errorf("expected the path to go without saying, got %q and %q", renderedPattern, detail)
	}

	renderedPattern, detail = grep.Render(grep.Args{Pattern: "hello", Path: "internal", Glob: "*.go"})
	if renderedPattern != "hello" || detail != "in internal (*.go)" {
		t.Errorf("expected a path and glob to be named, got %q and %q", renderedPattern, detail)
	}
}

func allowAll(string) error { return nil }
