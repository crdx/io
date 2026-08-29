package onboarding

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"crdx.org/io/cmd/oh/menu"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/style"
)

func TestOnboardingEdgeCasesMatchTheGoldens(t *testing.T) {
	tests := map[string]func(*testing.T, *bytes.Buffer) error{
		"first-run-anthropic": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				choose: menuChoices(output, 1, 0),
				login: func(chosen provider, presentAddress func(string)) error {
					if chosen.identifier != model.AnthropicProvider {
						t.Errorf("got provider %q", chosen.identifier)
					}
					presentAddress("https://example.test/authorise")
					return nil
				},
				refreshModels: func() error { return nil },
				getModels: func() []model.Choice {
					return []model.Choice{
						{Provider: model.AnthropicProvider, ID: "claude-opus-5", Name: "Claude Opus 5", EffortLevels: []string{"high"}},
						{Provider: model.CodexProvider, ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"medium"}},
					}
				},
				setInitialModel: func(selection string) error {
					if selection != "anthropic/claude-opus-5@high" {
						t.Errorf("saved %q", selection)
					}
					return nil
				},
			}
			return harry.castSpell()
		},
		"first-run-model-cancelled": func(_ *testing.T, output *bytes.Buffer) error {
			choiceIndex := 0
			harry := wizard{
				output: output,
				choose: func(prompt string, labels []string) (int, error) {
					_, _ = output.WriteString(menu.RenderMenu(prompt, labels, 0))
					choiceIndex++
					if choiceIndex == 2 {
						return 0, ErrCancelled
					}
					return 0, nil
				},
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return nil
				},
				refreshModels: func() error { return nil },
				getModels: func() []model.Choice {
					return []model.Choice{{
						Provider: model.CodexProvider, ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"medium"},
					}}
				},
				setInitialModel: func(string) error {
					return errors.New("model selection was unexpectedly stored")
				},
			}
			err := harry.castSpell()
			if !errors.Is(err, ErrCancelled) {
				return fmt.Errorf("got %w", err)
			}
			return nil
		},
		"first-run-refresh-failure": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				choose: menuChoices(output, 0),
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return nil
				},
				refreshModels: func() error { return errors.New("the model service is unavailable") },
			}
			err := harry.castSpell()
			if err == nil {
				return errors.New("expected model refresh to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"first-run-no-models": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				choose: menuChoices(output, 1),
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return nil
				},
				refreshModels: func() error { return nil },
				getModels: func() []model.Choice {
					return []model.Choice{{Provider: model.CodexProvider, ID: "gpt-5.6-sol", EffortLevels: []string{"medium"}}}
				},
			}
			err := harry.castSpell()
			if err == nil {
				return errors.New("expected the empty provider to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"first-run-config-write-failure": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				choose: menuChoices(output, 0, 0),
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return nil
				},
				refreshModels: func() error { return nil },
				getModels: func() []model.Choice {
					return []model.Choice{{
						Provider: model.CodexProvider, ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", EffortLevels: []string{"medium"},
					}}
				},
				setInitialModel: func(string) error { return errors.New("config is read-only") },
			}
			err := harry.castSpell()
			if err == nil {
				return errors.New("expected the config write to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
	}

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			restoreStyle := style.Init(&output)
			t.Cleanup(restoreStyle)

			if err := run(t, &output); err != nil {
				t.Fatal(err)
			}
			assertScreenGolden(t, name, output.String())
		})
	}
}
