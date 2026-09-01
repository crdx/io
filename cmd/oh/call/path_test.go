package call

import (
	"testing"

	"crdx.org/io/cmd/oh/work"
)

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
