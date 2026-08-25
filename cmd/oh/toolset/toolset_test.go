package toolset

import (
	"context"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/tool"
)

type fakeArgs struct {
	Path string `json:"path"`
}

func namedTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        name,
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, "" },
	).Plain(func(context.Context, fakeArgs) (string, error) {
		return "", nil
	})
}

func namesOf(tools []tool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, enabledTool := range tools {
		names = append(names, enabledTool.Name())
	}

	return names
}

func TestOnlyNamedToolsAreEnabled(t *testing.T) {
	availableTools := []tool.Tool{namedTool("read"), namedTool("grep"), namedTool("write")}

	allTools, err := Reduce(availableTools, nil)
	if err != nil || len(allTools) != len(availableTools) {
		t.Fatalf("expected every tool by default, got %v, %v", allTools, err)
	}

	enabledTools, err := Reduce(availableTools, []string{"write", "read", "read"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if names := namesOf(enabledTools); !slices.Equal(names, []string{"read", "write"}) {
		t.Errorf("expected read and write in canonical order, got %v", names)
	}

	if _, err := Reduce(availableTools, []string{"gone"}); err == nil {
		t.Error("expected an unavailable tool to be rejected")
	}
}

func TestEveryUnavailableToolIsNamedAtOnce(t *testing.T) {
	availableTools := []tool.Tool{namedTool("read")}

	_, err := Reduce(availableTools, []string{"gone", "read", "missing"})
	if err == nil || !strings.Contains(err.Error(), "tools not available: gone, missing") {
		t.Fatalf("expected both to be named, got %v", err)
	}
}
