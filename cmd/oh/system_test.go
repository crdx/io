package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/skill"
)

func systemRoot(t *testing.T) (*os.Root, string) {
	t.Helper()

	workspace := t.TempDir()
	root, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, workspace
}

func TestTheGlobalContextReplacesTheBuiltInOpeningButKeepsTheHarnessState(t *testing.T) {
	root, workspace := systemRoot(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	const configuredPrompt = "You are a deliberately custom assistant.\n"
	if err := os.WriteFile(globalContextPath(), []byte(configuredPrompt), 0o600); err != nil {
		t.Fatal(err)
	}

	got, contextFiles, err := loadContext(root, workspace, "/state/tmps/session", capRead, configuredPaths{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []contextFile{{name: "SYSTEM.md", body: configuredPrompt}}
	if !slices.Equal(contextFiles, wantFiles) {
		t.Errorf("got context files %v, want %v", contextFiles, wantFiles)
	}
	if !strings.Contains(got, "You are a deliberately custom assistant.") {
		t.Errorf("custom opening is missing: %q", got)
	}
	if strings.Contains(got, defaultGlobalContext) {
		t.Errorf("built-in opening was not replaced: %q", got)
	}
	if harness, opening := strings.Index(got, "# Scope"), strings.Index(got, "You are a deliberately"); harness > opening {
		t.Errorf("the harness does not come before the global context: %q", got)
	}
	for _, want := range []string{
		"The workspace (" + workspace + ") is read-only",
		"The .git directory within it (" + filepath.Join(workspace, ".git") + ") is read-only",
		"The bash tool is refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("runtime prompt does not contain %q: %q", want, got)
		}
	}
}

func TestAMissingGlobalContextUsesTheBuiltInOpening(t *testing.T) {
	root, workspace := systemRoot(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, _, err := loadContext(root, workspace, "/state/tmps/session", capRead, configuredPaths{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, defaultGlobalContext) {
		t.Errorf("built-in opening is missing: %q", got)
	}
	if harness, opening := strings.Index(got, "# Scope"), strings.Index(got, defaultGlobalContext); harness > opening {
		t.Errorf("the harness does not come before the built-in opening: %q", got)
	}
}

func TestContextFilesFollowTheOrderTheyAreConcatenatedIn(t *testing.T) {
	root, workspace := systemRoot(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	const configuredPrompt = "You are a deliberately custom assistant.\n"
	if err := os.WriteFile(globalContextPath(), []byte(configuredPrompt), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"AGENTS.md":       "Run the broad checks.",
		"AGENTS.local.md": "Never grant more access.",
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, contextFiles, err := loadContext(root, workspace, "/state/tmps/session", capRead, configuredPaths{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []contextFile{
		{name: "SYSTEM.md", body: configuredPrompt},
		{name: "AGENTS.md", body: "Run the broad checks."},
		{name: "AGENTS.local.md", body: "Never grant more access."},
	}
	if !slices.Equal(contextFiles, wantFiles) {
		t.Errorf("got context files %v, want %v", contextFiles, wantFiles)
	}
	system := strings.Index(got, "You are a deliberately custom assistant.")
	agents := strings.Index(got, "Run the broad checks.")
	local := strings.Index(got, "Never grant more access.")
	if system == -1 || agents == -1 || local == -1 || system >= agents || agents >= local {
		t.Errorf("prompt files are absent or out of order: %q", got)
	}
}

func TestConfiguredPathsAreDisclosedInTheHarnessContext(t *testing.T) {
	paths := configuredPaths{
		Read:  []string{"/reference"},
		Write: []string{"/output"},
		Exec:  []string{"/commands"},
	}
	got := harnessContext("/workspace", "/state/tmps/session", capRead|capWrite, paths)

	for _, want := range []string{
		"configured path /reference is read-only",
		"configured path /output is read-write and follows the workspace write state",
		"shell may execute files under /commands",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt does not contain %q: %q", want, got)
		}
	}
}

// Calling the work research only makes sense where nothing can be changed, so a writable workspace
// is not told to treat its tasks that way.
func TestTheResearchNoteIsOnlyMadeWhileTheWorkspaceIsReadOnly(t *testing.T) {
	const note = "consider any task you're given to be a research task"

	if got := harnessContext("/workspace", "/tmp/x", capRead, configuredPaths{}); !strings.Contains(got, note) {
		t.Errorf("a read-only workspace was not told to research: %q", got)
	}

	if got := harnessContext("/workspace", "/tmp/x", capRead|capWrite, configuredPaths{}); strings.Contains(got, note) {
		t.Errorf("a writable workspace was told to research anyway: %q", got)
	}
}

// The scratch mapping is read out to the user rather than given to a tool, so it is written the way
// they would say it themselves.
func TestTheScratchMappingIsWrittenWithATilde(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	got := harnessContext("/workspace", "/home/alice/.local/state/org.crdx/oh/tmp/0d3f", capRead, configuredPaths{})

	for _, want := range []string{
		"/tmp maps to ~/.local/state/org.crdx/oh/tmp/0d3f on the user's machine",
		"/tmp/result.png → ~/.local/state/org.crdx/oh/tmp/0d3f/result.png",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheSkillCatalogueIsAppendedToTheContext(t *testing.T) {
	root, workspace := systemRoot(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, _, err := loadContext(root, workspace, "/state/tmps/session", capRead, configuredPaths{}, []skill.Skill{{
		Name: "pdf", Description: "Work with PDFs.", Location: "/skills/pdf/SKILL.md",
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<available_skills>", "<name>pdf</name>", "/skills/pdf/SKILL.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt does not contain %q: %q", want, got)
		}
	}
}
