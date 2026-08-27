package cli

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/model"
)

const helpProcessVariable = "OH_TEST_HELP_PROCESS"

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestUsageMatchesTheGolden(t *testing.T) {
	if os.Getenv(helpProcessVariable) != "" {
		os.Args = []string{"oh", "--help"}
		Bind()
		return
	}

	//nolint:gosec // rerun this test binary as its help subprocess
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestUsageMatchesTheGolden$")
	command.Env = append(os.Environ(), helpProcessVariable+"=1")
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
		t.Fatalf("help process returned %v", err)
	}

	goldenPath := filepath.Join("testdata", "usage.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, output, 0o600); err != nil { //nolint:gosec // fixed testdata path
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != string(want) {
		t.Errorf("usage differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, output, want)
	}
}

func modelCachePath() string {
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "models.json")
}

func bind(t *testing.T, arguments ...string) Input {
	t.Helper()

	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	os.Args = append([]string{"oh"}, arguments...)

	return *Bind()
}

func parseOptions(t *testing.T, arguments ...string) Options {
	t.Helper()

	settledOptions, err := bind(t, arguments...).Parse(modelCachePath())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return settledOptions
}

func useCachedModels(t *testing.T) {
	t.Helper()

	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := modelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	data := []byte(`{"version":2,"providers":{"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["high","max"],"output":384000}]}}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEveryOptionIsRead(t *testing.T) {
	useCachedModels(t)

	parsedOptions := parseOptions(
		t,
		"-c", "r",
		"-d", "somewhere",
		"-m", "deepseek@hi",
		"-t", "read",
		"--tool", "grep",
		"Use", "the", "brief.",
	)

	if parsedOptions.Caps != caps.Read {
		t.Errorf("expected reading alone, got %s", parsedOptions.Caps.Flags())
	}

	if parsedOptions.WorkspaceDir != "somewhere" {
		t.Errorf("expected the directory, got %q", parsedOptions.WorkspaceDir)
	}

	if parsedOptions.Provider != model.OpencodeGoProvider || parsedOptions.Model != "deepseek-v4-pro" || parsedOptions.Effort != "high" {
		t.Errorf("expected opencode-go/deepseek-v4-pro@high, got %s/%s@%s", parsedOptions.Provider, parsedOptions.Model, parsedOptions.Effort)
	}

	if !slices.Equal(parsedOptions.Tools, []string{"read", "grep"}) {
		t.Errorf("expected read and grep, got %v", parsedOptions.Tools)
	}
	if parsedOptions.Message != "Use the brief." {
		t.Errorf("expected the caller's opening message, got %q", parsedOptions.Message)
	}

	id := "0347juX1xcrL9W0QKJe0cs"

	if parsedOptions := parseOptions(t, "-r", id); parsedOptions.Session != id || !parsedOptions.Resuming() {
		t.Errorf("expected the session, got %q", parsedOptions.Session)
	}
}

func TestModelSelectionRequiresModelAndEffort(t *testing.T) {
	for _, selection := range []string{"model", "model@", "@high", "model@high@extra"} {
		if _, err := (Input{inputFlags: inputFlags{Model: selection}}).Parse(modelCachePath()); err == nil {
			t.Errorf("expected %q to be rejected", selection)
		}
	}
}

func TestANewConversationMayStartFromASession(t *testing.T) {
	parsedOptions := parseOptions(t, "--from", "chosen-lobster", "carry", "on")

	if !parsedOptions.StartingFromSession() || parsedOptions.SourceSession != "chosen-lobster" {
		t.Errorf("expected the source session, got %q", parsedOptions.SourceSession)
	}
	if parsedOptions.Resuming() {
		t.Error("expected starting from a session not to count as resuming")
	}
	if parsedOptions.Message != "carry on" {
		t.Errorf("expected the prompt beside it, got %q", parsedOptions.Message)
	}
}

func TestASessionMayBeResumedWithAPromptBesideIt(t *testing.T) {
	for _, argument := range []string{"-r", "--resume"} {
		parsedOptions := parseOptions(t, argument, "0347juX1xcrL9W0QKJe0cs", "carry", "on")

		if !parsedOptions.Resuming() {
			t.Errorf("expected %s to resume the session", argument)
		}

		if parsedOptions.Message != "carry on" {
			t.Errorf("expected the prompt beside %s, got %q", argument, parsedOptions.Message)
		}
	}
}

func TestTheVersionIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--version", "-v"} {
		if !bind(t, argument).Version {
			t.Errorf("expected the version to be asked for by %s", argument)
		}
	}
}

func TestTheModelListIsAskedForOnItsOwn(t *testing.T) {
	for _, argument := range []string{"--list", "-l"} {
		if !bind(t, argument).List {
			t.Errorf("expected the model list to be asked for by %s", argument)
		}
	}
}

func TestTheSessionPickerIsAskedForByBareResumeOption(t *testing.T) {
	for _, argument := range []string{"-r", "--resume"} {
		if !bind(t, argument).IsSessionPicker {
			t.Errorf("expected a bare %s to ask for the session picker", argument)
		}
	}
}

func TestWhateverIsLeftOverIsTheFirstThingSaid(t *testing.T) {
	parsedOptions := parseOptions(t, "why", "does", "the", "spinner", "stutter")

	if parsedOptions.Message != "why does the spinner stutter" {
		t.Errorf("expected the words back as one, got %q", parsedOptions.Message)
	}

	if parsedOptions.WorkspaceDir != "." {
		t.Errorf("expected the current directory, got %q", parsedOptions.WorkspaceDir)
	}
}

func TestTheWorkingDirectoryIsNotTakenFromThePrompt(t *testing.T) {
	parsedOptions := parseOptions(t, "read", "main.go", "-d", "/tmp")

	if parsedOptions.WorkspaceDir != "/tmp" {
		t.Errorf("expected the directory to come from the option, got %q", parsedOptions.WorkspaceDir)
	}

	if parsedOptions.Message != "read main.go" {
		t.Errorf("expected the rest to be the prompt, got %q", parsedOptions.Message)
	}
}

func TestTheDefaultCapabilitiesAreReadingAndTheShell(t *testing.T) {
	parsedOptions := parseOptions(t)

	if got := parsedOptions.Caps.Flags(); got != "rx" {
		t.Errorf("expected rx, got %q", got)
	}
	if parsedOptions.WereCapsChosen {
		t.Error("expected the default capabilities to count as unchosen")
	}

	if parsedOptions.Message != "" {
		t.Errorf("expected nothing said, got %q", parsedOptions.Message)
	}

	if len(parsedOptions.Tools) != 0 {
		t.Errorf("expected every tool by default, got %v", parsedOptions.Tools)
	}
}

func TestCapabilitiesAreReadAsTheLettersTheyAreSpelledWith(t *testing.T) {
	for _, capString := range []string{"rwxgs", "sgxwr", "wxgs"} {
		currentCaps, err := caps.Parse(capString)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", capString, err)
		}

		if got := currentCaps.Flags(); got != caps.AllFlags {
			t.Errorf("%s: expected it written back as %s, got %q", capString, caps.AllFlags, got)
		}
	}

	if _, err := caps.Parse("rwz"); err == nil {
		t.Error("expected a letter naming no capability to be refused")
	}
}

func TestReadingIsAlwaysGranted(t *testing.T) {
	grantedCaps, err := caps.Parse("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if got := grantedCaps.Flags(); got != "r" {
		t.Errorf("expected r, got %q", got)
	}
}

func TestAWorkspaceCannotBeGivenWhenContinuingAStoredSession(t *testing.T) {
	for name, input := range map[string]Input{
		"resume": {inputFlags: inputFlags{Session: "one", WorkspaceDir: "somewhere"}},
		"from":   {inputFlags: inputFlags{WorkspaceDir: "somewhere"}, SourceSession: "one"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := input.Parse(modelCachePath()); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestASessionCannotBeResumedAndUsedAsTheSourceTogether(t *testing.T) {
	input := Input{inputFlags: inputFlags{Session: "one"}, SourceSession: "another"}
	if _, err := input.Parse(modelCachePath()); err == nil {
		t.Error("expected an error")
	}
}

func TestAModeNamedOnTheCommandLineCountsAsChosen(t *testing.T) {
	opts := Input{inputFlags: inputFlags{Session: "one", Caps: "rx"}}
	settledOptions, err := opts.Parse(modelCachePath())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if settledOptions.Caps.Has(caps.Write) {
		t.Error("expected writing to be held back")
	}
	if !settledOptions.WereCapsChosen {
		t.Error("expected the named capabilities to count as chosen")
	}
}
