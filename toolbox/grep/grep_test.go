package grep_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/toolbox/grep"
)

func rooted(t *testing.T, files map[string]string) *os.Root {
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

	return root
}

func exec(t *testing.T, root *os.Root, arguments string) (string, error) {
	t.Helper()

	call, err := grep.New(root).Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return call.Exec()
}

func TestAMatchIsReportedWithItsPathAndLine(t *testing.T) {
	root := rooted(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})

	output, err := exec(t, root, `{"pattern":"func main"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "main.go:3:func main() {}" {
		t.Errorf("expected the path, line and text, got %q", output)
	}
}

func TestAGlobNarrowsWhatIsSearched(t *testing.T) {
	root := rooted(t, map[string]string{
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

func TestAPatternThatWillNotCompileIsRefused(t *testing.T) {
	root := rooted(t, map[string]string{"main.go": "hello\n"})

	_, err := exec(t, root, `{"pattern":"("}`)
	if err == nil {
		t.Fatal("expected an invalid pattern to be refused")
	}

	if !strings.Contains(err.Error(), "invalid pattern") {
		t.Errorf("expected the refusal to name the pattern, got %q", err)
	}
}

func TestASearchThatFindsNothingSaysSo(t *testing.T) {
	root := rooted(t, map[string]string{"main.go": "hello\n"})

	output, err := exec(t, root, `{"pattern":"goodbye"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "(no matches)" {
		t.Errorf("expected no matches to say so, got %q", output)
	}
}

func TestHittingTheCapIsSaidOutLoud(t *testing.T) {
	root := rooted(t, map[string]string{
		"big.txt": strings.Repeat("hello\n", 150),
	})

	output, err := exec(t, root, `{"pattern":"hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "narrow the search") {
		t.Errorf("expected the cap to be reported, got the last of %q", output[len(output)-80:])
	}
}

func TestRenderSaysNothingOfTheWorkingDirectory(t *testing.T) {
	rendered := grep.Render(grep.Args{Pattern: "hello", Path: "."})
	if rendered != "hello" {
		t.Errorf("expected the path to go without saying, got %q", rendered)
	}

	rendered = grep.Render(grep.Args{Pattern: "hello", Path: "internal", Glob: "*.go"})
	if rendered != "hello in internal matching *.go" {
		t.Errorf("expected a path and glob to be named, got %q", rendered)
	}
}
