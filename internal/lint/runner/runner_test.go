package runner

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func everyFunction(file File) []Diagnostic {
	var diagnostics []Diagnostic
	for _, declaration := range file.Syntax.Decls {
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
			diagnostics = append(diagnostics, file.Report(function.Name, "found "+function.Name.Name))
		}
	}
	return diagnostics
}

func write(t *testing.T, directory string, name string, source string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return path
}

func TestRunReportsEveryDiagnosticInOrder(t *testing.T) {
	directory := t.TempDir()
	second := write(t, directory, "second.go", "package example\n\nfunc beta() {}\n")
	first := write(t, directory, "first.go", "package example\n\nfunc alpha() {}\n")

	output := &strings.Builder{}
	errorOutput := &strings.Builder{}
	if Run(output, errorOutput, "rule", everyFunction, []string{second, first}) {
		t.Error("expected the run to fail")
	}
	if errorOutput.Len() != 0 {
		t.Errorf("unexpected error output %q", errorOutput.String())
	}

	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), output.String())
	}
	if !strings.HasSuffix(lines[0], "first.go:3:6: found alpha (rule)") {
		t.Errorf("got %q, want the first file first", lines[0])
	}
	if !strings.HasSuffix(lines[1], "second.go:3:6: found beta (rule)") {
		t.Errorf("got %q, want the second file second", lines[1])
	}
}

func TestRunAcceptsACleanFile(t *testing.T) {
	path := write(t, t.TempDir(), "clean.go", "package example\n\nvar thing = 1\n")

	output := &strings.Builder{}
	errorOutput := &strings.Builder{}
	if !Run(output, errorOutput, "rule", everyFunction, []string{path}) {
		t.Errorf("expected the run to pass, got %q %q", output.String(), errorOutput.String())
	}
}

func TestRunFailsOnAFileItCannotRead(t *testing.T) {
	output := &strings.Builder{}
	errorOutput := &strings.Builder{}
	if Run(output, errorOutput, "rule", everyFunction, []string{filepath.Join(t.TempDir(), "missing.go")}) {
		t.Error("expected the run to fail")
	}
	if !strings.Contains(errorOutput.String(), "missing.go") {
		t.Errorf("got %q, want the missing file named", errorOutput.String())
	}
}

func TestRunFailsOnAFileItCannotParse(t *testing.T) {
	path := write(t, t.TempDir(), "broken.go", "package")

	output := &strings.Builder{}
	errorOutput := &strings.Builder{}
	if Run(output, errorOutput, "rule", everyFunction, []string{path}) {
		t.Error("expected the run to fail")
	}
	if !strings.Contains(errorOutput.String(), "broken.go") {
		t.Errorf("got %q, want the unparsable file named", errorOutput.String())
	}
}

func TestTheFileIsNamedAsItWasGiven(t *testing.T) {
	directory := t.TempDir()
	path := write(t, directory, "named.go", "package example\n")

	var seen string
	_, err := Check(func(file File) []Diagnostic {
		seen = file.Name
		return nil
	}, path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen != path {
		t.Errorf("got %q, want %q", seen, path)
	}
}
