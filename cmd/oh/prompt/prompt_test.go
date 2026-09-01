package prompt

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/work"
)

func systemWorkspace(t *testing.T) *work.Space {
	t.Helper()

	workspace := work.At(t.TempDir())
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	return workspace
}

func configDir() string {
	return filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "org.crdx", "oh")
}

func globalContextPath() string {
	return filepath.Join(configDir(), globalContextName)
}

func loadTestContext(workspace *work.Space, skills []skill.Skill) (string, []File, error) {
	return Load(Config{
		GlobalPath:  globalContextPath(),
		Workspace:   workspace,
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
		Skills:      skills,
	})
}

func TestTheGlobalContextReplacesTheBuiltInOpeningButKeepsTheHarnessState(t *testing.T) {
	workspace := systemWorkspace(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	const configuredGlobalContext = "You are a deliberately custom assistant.\n"
	if err := os.WriteFile(globalContextPath(), []byte(configuredGlobalContext), 0o600); err != nil {
		t.Fatal(err)
	}

	got, contextFiles, err := loadTestContext(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantcontextFiles := []File{{Name: "SYSTEM.md", Body: configuredGlobalContext}}
	if !slices.Equal(contextFiles, wantcontextFiles) {
		t.Errorf("got context files %v, want %v", contextFiles, wantcontextFiles)
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
		"The workspace (" + workspace.GetDir() + ") is read-only",
		"The .git directory within it (" + filepath.Join(workspace.GetDir(), ".git") + ") is read-only",
		"The bash tool is refused",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("runtime prompt does not contain %q: %q", want, got)
		}
	}
}

func TestAMissingGlobalContextUsesTheBuiltInOpening(t *testing.T) {
	workspace := systemWorkspace(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, _, err := loadTestContext(workspace, nil)
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

func TestContextcontextFilesFollowTheOrderTheyAreConcatenatedIn(t *testing.T) {
	workspace := systemWorkspace(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	const configuredGlobalContext = "You are a deliberately custom assistant.\n"
	if err := os.WriteFile(globalContextPath(), []byte(configuredGlobalContext), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"AGENTS.md":       "Run the broad checks.",
		"AGENTS.local.md": "Never grant more access.",
	} {
		if err := os.WriteFile(filepath.Join(workspace.GetDir(), name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, contextFiles, err := loadTestContext(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantcontextFiles := []File{
		{Name: "SYSTEM.md", Body: configuredGlobalContext},
		{Name: "AGENTS.md", Body: "Run the broad checks."},
		{Name: "AGENTS.local.md", Body: "Never grant more access."},
	}
	if !slices.Equal(contextFiles, wantcontextFiles) {
		t.Errorf("got context files %v, want %v", contextFiles, wantcontextFiles)
	}
	system := strings.Index(got, "You are a deliberately custom assistant.")
	agents := strings.Index(got, "Run the broad checks.")
	local := strings.Index(got, "Never grant more access.")
	if system == -1 || agents == -1 || local == -1 || system >= agents || agents >= local {
		t.Errorf("prompt files are absent or out of order: %q", got)
	}
}

func TestConfiguredPathsAreDisclosedInTheHarnessContext(t *testing.T) {
	paths := shell.Paths{
		Read:  []string{"/reference"},
		Write: []string{"/output"},
		Exec:  []string{"/commands"},
	}
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Write,
		ExtraPaths:  paths,
	})

	for _, want := range []string{
		"configured path /reference is read-only",
		"configured path /output is read-write and follows the workspace write state",
		"shell may execute files at or under /commands",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt does not contain %q: %q", want, got)
		}
	}
}

func TestTheHarnessDisclosesTheSessionName(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "brave-otter",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	if !strings.Contains(got, "Your session is named brave-otter") {
		t.Errorf("harness context does not contain the session name: %q", got)
	}
}

func TestTheHarnessGivesTheSessionItsAnimalPersonality(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "brave-otter",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	if !strings.Contains(got, "Adopt the personality of the animal in your session name") {
		t.Errorf("harness context does not give the session its animal personality: %q", got)
	}
}

func TestTheHarnessDisclosesWebAccess(t *testing.T) {
	withheld := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})
	if !strings.Contains(withheld, "web search and fetch tools are refused") {
		t.Errorf("harness context does not disclose withheld web access: %q", withheld)
	}

	granted := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Web,
		ExtraPaths:  shell.Paths{},
	})
	if !strings.Contains(granted, "web search and fetch tools are granted external network access") {
		t.Errorf("harness context does not disclose granted web access: %q", granted)
	}
}

func TestTheHarnessDisclosesPrivateLoopbackNetworking(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	for _, want := range []string{
		"private loopback interface",
		"127.0.0.1 and ::1",
		"host's loopback interface and external networks are unreachable",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheScratchMappingIsWrittenInFull(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	scratch := "/home/alice/.local/state/org.crdx/oh/farm/0d3f"
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      scratch,
		HomeDir:     "/home/alice/.local/state/org.crdx/oh/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	for _, want := range []string{
		"It maps to " + scratch + " on the user's machine",
		"/tmp/foo.png → " + scratch + "/foo.png",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheHarnessDisclosesTheShellHome(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/tmp/x",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	for _, want := range []string{
		"path can only access the workspace, private home, and /tmp",
		"HOME is /state/home",
		"A tilde (~) for you is not the same as for the user",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}
}

func TestTheHarnessNeverAbbreviatesAPathToATilde(t *testing.T) {
	home := "/home/alice"
	t.Setenv("HOME", home)

	got := harnessContext(Config{
		Workspace:   work.At(filepath.Join(home, "workspace")),
		SessionName: "session-id",
		TmpDir:      filepath.Join(home, ".local", "state", "org.crdx", "oh", "farm", "0d3f"),
		HomeDir:     filepath.Join(home, ".local", "state", "org.crdx", "oh", "home"),
		CurrentCaps: caps.Read | caps.Write | caps.Shell,
		ExtraPaths:  shell.Paths{Read: []string{filepath.Join(home, "reference")}, Write: []string{filepath.Join(home, "output")}, Exec: []string{filepath.Join(home, "commands")}},
	})

	for line := range strings.SplitSeq(got, "\n") {
		if strings.Contains(line, "~/") {
			t.Errorf("harness context abbreviates a path: %q", line)
		}
	}
}

func TestTheSkillCatalogueIsAppendedToTheContext(t *testing.T) {
	workspace := systemWorkspace(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, _, err := loadTestContext(workspace, []skill.Skill{{
		Name:        "pdf",
		Description: "Work with PDFs.",
		Location:    "/skills/pdf/SKILL.md",
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

func TestPromptSeparatesTheWorkspaceFromTmp(t *testing.T) {
	system := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read,
		ExtraPaths:  shell.Paths{},
	})

	if want := "The workspace (/workspace) is " + filesystem(false); !strings.Contains(system, want) {
		t.Errorf("expected the workspace to be reported as %q, got %q", want, system)
	}

	if want := "The .git directory within it (/workspace/.git) is " + filesystem(false); !strings.Contains(system, want) {
		t.Errorf("expected the history to be reported as %q, got %q", want, system)
	}

	if !strings.Contains(system, "which you can always read and write") {
		t.Errorf("expected the scratch to be writable whatever the workspace is, got %q", system)
	}

	if !strings.Contains(system, "It maps to /state/farm/session on the user's machine") {
		t.Errorf("expected the scratch backing directory to be reported, got %q", system)
	}

	if !strings.Contains(system, "/tmp/foo.png → /state/farm/session/foo.png") {
		t.Errorf("expected an example translated scratch path, got %q", system)
	}

	if strings.Contains(system, "including /tmp") {
		t.Errorf("the workspace mode still claims to include /tmp: %q", system)
	}
}

func TestPromptStatesWhetherTheShellCanRun(t *testing.T) {
	for name, test := range map[string]struct {
		currentCaps caps.Set
		granted     bool
	}{
		"granted": {caps.Read | caps.Shell, true},
		"refused": {caps.Read, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := harnessContext(Config{
				Workspace:   work.At("/workspace"),
				SessionName: "session-id",
				TmpDir:      "/state/farm/session",
				HomeDir:     "/state/home",
				CurrentCaps: test.currentCaps,
				ExtraPaths:  shell.Paths{},
			})

			if want := "The bash tool is " + shellAccess(test.granted); !strings.Contains(got, want) {
				t.Errorf("expected %q in %q", want, got)
			}

			if unwanted := "The bash tool is " + shellAccess(!test.granted); strings.Contains(got, unwanted) {
				t.Errorf("expected no %q in %q", unwanted, got)
			}
		})
	}
}

func TestAWaivedSandboxIsDisclosedRatherThanImplied(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Shell,
		Yolo:        true,
	})

	for _, want := range []string{
		"# No Sandbox",
		"the bash tool runs with no sandbox at all",
		"There is no sandbox, so a command reaches whatever network this machine reaches",
		"/tmp is the machine's own /tmp",
		"Your persistent scratch space is /state/farm/session",
		"The bash tool is granted, and runs unconfined",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("harness context does not contain %q: %q", want, got)
		}
	}

	for _, unwanted := range []string{
		"private loopback interface",
		"external networks are unreachable",
		"It maps to /state/farm/session on the user's machine",
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context still claims %q: %q", unwanted, got)
		}
	}
}

func TestASandboxedSessionIsNeverToldThereIsNoSandbox(t *testing.T) {
	got := harnessContext(Config{
		Workspace:   work.At("/workspace"),
		SessionName: "session-id",
		TmpDir:      "/state/farm/session",
		HomeDir:     "/state/home",
		CurrentCaps: caps.Read | caps.Shell,
	})

	for _, unwanted := range []string{"# No Sandbox", "unconfined", "--yolo"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("harness context contains %q: %q", unwanted, got)
		}
	}
}
