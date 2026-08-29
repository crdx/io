package onboarding

import (
	"bytes"
	"errors"
	"flag"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/style"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestFirstRunOnboardingMatchesTheGolden(t *testing.T) {
	var output bytes.Buffer
	restoreStyle := style.Init(&output)
	t.Cleanup(restoreStyle)

	choices := []int{0, 0}
	choiceIndex := 0
	var savedSelection string
	modelsWereRefreshed := false

	onboarding := wizard{
		output: &output,
		choose: func(prompt string, labels []string) (int, error) {
			chosen := choices[choiceIndex]
			choiceIndex++
			_, err := output.WriteString(picker.RenderMenu(prompt, labels, chosen))
			return chosen, err
		},
		login: func(chosen provider, presentAddress func(string)) error {
			if chosen.identifier != model.CodexProvider {
				t.Errorf("got provider %q", chosen.identifier)
			}
			presentAddress("https://example.test/authorise")
			return nil
		},
		refreshModels: func() error {
			modelsWereRefreshed = true
			return nil
		},
		getModels: func() []model.Choice {
			if !modelsWereRefreshed {
				t.Error("models were read before they were refreshed")
			}
			return []model.Choice{
				{Provider: model.CodexProvider, ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"low", "medium", "high"}},
				{Provider: model.CodexProvider, ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", EffortLevels: []string{"medium", "high"}},
				{Provider: model.CodexProvider, ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", EffortLevels: []string{"medium", "high"}},
				{Provider: model.AnthropicProvider, ID: "claude-opus-5", Name: "Claude Opus 5", EffortLevels: []string{"high"}},
			}
		},
		setInitialModel: func(selection string) error {
			savedSelection = selection
			return nil
		},
	}

	if err := onboarding.castSpell(); err != nil {
		t.Fatal(err)
	}
	if savedSelection != "codex/gpt-5.6-sol@medium" {
		t.Errorf("saved %q", savedSelection)
	}

	assertScreenGolden(t, "first-run", output.String())
}

func TestOnboardingWritesASelectionThatOrdinaryStartupCanLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	choices := []int{0, 0}
	choiceIndex := 0

	onboarding := wizard{
		output: &bytes.Buffer{},
		choose: func(_ string, _ []string) (int, error) {
			chosen := choices[choiceIndex]
			choiceIndex++
			return chosen, nil
		},
		login:         func(provider, func(string)) error { return nil },
		refreshModels: func() error { return nil },
		getModels: func() []model.Choice {
			return []model.Choice{{
				Provider:     model.CodexProvider,
				ID:           "gpt-5.6-sol",
				EffortLevels: []string{"low", "medium", "high"},
			}}
		},
		setInitialModel: func(selection string) error {
			_, err := setInitialModel(path, selection)
			return err
		},
	}

	if err := onboarding.castSpell(); err != nil {
		t.Fatal(err)
	}

	settings, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex/gpt-5.6-sol@medium"}
	if !slices.Equal(settings.Model.RoundRobin, want) {
		t.Errorf("got model rotation %v, want %v", settings.Model.RoundRobin, want)
	}
}

func TestOnboardingOffersAnotherProviderAfterLoginFails(t *testing.T) {
	var output bytes.Buffer
	chosenProviders := []int{0, 1}
	providerChoices := 0
	loginAttempts := 0

	onboarding := wizard{
		output: &output,
		choose: func(prompt string, _ []string) (int, error) {
			if prompt == "Choose a model:" {
				return 0, nil
			}
			chosen := chosenProviders[providerChoices]
			providerChoices++
			return chosen, nil
		},
		login: func(_ provider, _ func(string)) error {
			loginAttempts++
			if loginAttempts == 1 {
				return errors.New("authorisation was refused")
			}
			return nil
		},
		refreshModels: func() error { return nil },
		getModels: func() []model.Choice {
			return []model.Choice{{
				Provider:     model.AnthropicProvider,
				ID:           "claude-opus-5",
				EffortLevels: []string{"high"},
			}}
		},
		setInitialModel: func(string) error { return nil },
	}

	if err := onboarding.castSpell(); err != nil {
		t.Fatal(err)
	}
	if loginAttempts != 2 {
		t.Errorf("got %d login attempts", loginAttempts)
	}
	if !strings.Contains(output.String(), "Couldn’t sign in: authorisation was refused") {
		t.Errorf("failure was not shown in %q", output.String())
	}
}

func TestNamedLoginSkipsTheProviderPicker(t *testing.T) {
	var output bytes.Buffer
	var loggedInTo string
	harry := wizard{
		output: &output,
		choose: func(string, []string) (int, error) {
			t.Fatal("provider picker was shown")
			return 0, nil
		},
		login: func(chosen provider, _ func(string)) error {
			loggedInTo = chosen.identifier
			return nil
		},
	}

	if _, err := harry.chooseProvider(model.AnthropicProvider); err != nil {
		t.Fatal(err)
	}
	if loggedInTo != model.AnthropicProvider {
		t.Errorf("logged in to %q", loggedInTo)
	}
	if output.String() != "✓ Signed in\n\n" {
		t.Errorf("got output %q", output.String())
	}
}

func TestNamedLoginRejectsAnUnknownProvider(t *testing.T) {
	harry := wizard{}
	if _, err := harry.chooseProvider("somewhere"); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("got %v", err)
	}
}

func TestOnboardingIsOnlyRequiredWithoutAnotherModelSelection(t *testing.T) {
	if !isRequired(Options{}, nil) {
		t.Error("expected a first run to need onboarding")
	}

	tests := map[string]struct {
		options          Options
		configuredModels []string
	}{
		"alternate endpoint": {options: Options{EndpointURL: "http://localhost"}},
		"requested model":    {options: Options{RequestedModel: "codex/model"}},
		"resumed session":    {options: Options{ResumedSession: "session"}},
		"configured model":   {configuredModels: []string{"codex/model@high"}},
	}
	for name, test := range tests {
		if isRequired(test.options, test.configuredModels) {
			t.Errorf("%s unexpectedly needed onboarding", name)
		}
	}
}

func visibleTranscript(rendered string) string {
	rendered = link.Plain(rendered)
	rendered = strings.ReplaceAll(rendered, "\r\n", "\n")

	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}
