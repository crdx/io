package call

import (
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/tool"
)

func TestAContinuedCallHasItsPathPrefixesShortened(t *testing.T) {
	const workspaceDir = "/home/alice/project"
	rendering := agent.FallbackRendering{
		Continuation: []tool.CallRendering{{
			Name:      "bash",
			Subject:   workspaceDir + "/check",
			Qualifier: workspaceDir + "/detail",
			Emphasis:  tool.Emphasis{Source: workspaceDir + "/source"},
		}},
	}

	shortened := shortenPaths(rendering, work.At(workspaceDir))
	part := shortened.Continuation[0]
	if part.Subject != "check" || part.Qualifier != "detail" || part.Emphasis.Source != "source" {
		t.Errorf("got %#v, want every continuation path shortened", part)
	}
}

func TestWorkspacePathPrefixesAreShortened(t *testing.T) {
	const workspaceDir = "/home/alice/project"
	t.Setenv("HOME", "/home/alice")

	tests := map[string]string{
		workspaceDir:                      "",
		"~/project":                       "",
		workspaceDir + " **/*.go":         "**/*.go",
		"~/project **/*.go":               "**/*.go",
		workspaceDir + "/cmd/oh/draw.go":  "cmd/oh/draw.go",
		"~/project/cmd/oh/draw.go":        "cmd/oh/draw.go",
		"/home/alice/other.go":            "~/other.go",
		"/home/alice/projectile/other.go": "~/projectile/other.go",
	}
	for value, want := range tests {
		if got := shortenPathPrefix(value, work.At(workspaceDir)); got != want {
			t.Errorf("shortenPathPrefix(%q) = %q, want %q", value, got, want)
		}
	}
}
