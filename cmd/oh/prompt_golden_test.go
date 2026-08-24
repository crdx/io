package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/prompt"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
)

func TestTheCompleteSystemPromptMatchesTheGolden(t *testing.T) {
	workspace := t.TempDir()
	for name, body := range map[string]string{
		"AGENTS.md":       "Follow the project rules.",
		"AGENTS.local.md": "Prefer the local rules.",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	globalPath := filepath.Join(t.TempDir(), "SYSTEM.md")
	if err := os.WriteFile(globalPath, []byte("You are the golden test assistant."), 0o600); err != nil {
		t.Fatal(err)
	}

	got, _, err := prompt.Load(prompt.Config{
		GlobalPath:   globalPath,
		Root:         root,
		WorkspaceDir: "/workspace",
		SessionName:  "brave-otter",
		TmpDir:       "/state/tmps/brave-otter",
		HomeDir:      "/state/home",
		CurrentCaps:  caps.Read | caps.Write | caps.Git | caps.Shell | caps.Background,
		ExtraPaths: shell.Paths{
			Read:  []string{"/reference"},
			Write: []string{"/output"},
			Exec:  []string{"/commands"},
		},
		Skills: []skill.Skill{{
			Name:        "golden",
			Description: "Exercise complete prompt assembly.",
			Location:    "/skills/golden/SKILL.md",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got = strings.ReplaceAll(got, "127.0.0.1", "<loopback>")

	goldenPath := filepath.Join("testdata", "output", "context.prompt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("system prompt differs from %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}
}
