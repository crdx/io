package onboarding

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/internal/oauth"
)

func TestAuthenticationFlowsMatchTheGoldens(t *testing.T) {
	tests := map[string]func(*testing.T, *bytes.Buffer) error{
		"login-picker": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				choose: menuChoices(output, 0),
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return nil
				},
			}
			err := harry.chooseProvider("")
			return err
		},
		"login-anthropic": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				login: func(chosen provider, presentAddress func(string)) error {
					if chosen.identifier != model.AnthropicProvider {
						t.Errorf("got provider %q", chosen.identifier)
					}
					presentAddress("https://example.test/authorise")
					return nil
				},
			}
			err := harry.chooseProvider(model.AnthropicProvider)
			return err
		},
		"login-browser-failure": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise?token=one")
					return nil
				},
				openBrowser: func(string) error { return errors.New("no browser is available") },
			}
			err := harry.chooseProvider(model.CodexProvider)
			return err
		},
		"login-long-url": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			address := "https://auth.example.test/oauth/authorize?client_id=oh-desktop&code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ&code_challenge_method=S256&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&response_type=code&scope=openid%20profile%20email%20offline_access"
			var openedAddress string
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress(address)
					return nil
				},
				openBrowser: func(got string) error {
					if !strings.Contains(link.Plain(output.String()), address) {
						t.Error("browser was opened before the complete address was printed")
					}
					openedAddress = got
					return nil
				},
			}
			if err := harry.chooseProvider(model.CodexProvider); err != nil {
				return err
			}
			if openedAddress != address {
				t.Errorf("opened %q", openedAddress)
			}
			return nil
		},
		"login-direct-failure": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					return errors.New("authorisation was refused")
				},
			}
			err := harry.chooseProvider(model.AnthropicProvider)
			if err == nil {
				return errors.New("expected direct login to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"login-pasted-redirect": func(_ *testing.T, output *bytes.Buffer) error {
			redirect := "http://localhost:1455/auth/callback?code=accepted&state=expected"
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					_, _ = fmt.Fprintln(output, redirect)
					_, err := oauth.CodeFromRedirect(redirect, "expected")
					return err
				},
			}
			err := harry.chooseProvider(model.CodexProvider)
			return err
		},
		"login-malformed-redirect": func(_ *testing.T, output *bytes.Buffer) error {
			redirect := "not a complete URL"
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					_, _ = fmt.Fprintln(output, redirect)
					_, err := oauth.CodeFromRedirect(redirect, "expected")
					return err
				},
			}
			err := harry.chooseProvider(model.CodexProvider)
			if err == nil {
				return errors.New("expected malformed redirect to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"login-redirect-state-mismatch": func(_ *testing.T, output *bytes.Buffer) error {
			redirect := "http://localhost:1455/auth/callback?code=accepted&state=wrong"
			harry := wizard{
				output: output,
				login: func(_ provider, presentAddress func(string)) error {
					presentAddress("https://example.test/authorise")
					_, _ = fmt.Fprintln(output, redirect)
					_, err := oauth.CodeFromRedirect(redirect, "expected")
					return err
				},
			}
			err := harry.chooseProvider(model.CodexProvider)
			if err == nil {
				return errors.New("expected mismatched redirect to fail")
			}
			_, writeError := fmt.Fprintln(output, err)
			return writeError
		},
		"login-opencode-go": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				login: func(chosen provider, _ func(string)) error {
					if chosen.identifier != model.OpencodeGoProvider {
						t.Errorf("got provider %q", chosen.identifier)
					}
					return storeOpenCodeGoKey(
						strings.NewReader("secret\n"),
						output,
						filepath.Join(t.TempDir(), "auth.json"),
						func(string) error { return nil },
					)
				},
			}
			err := harry.chooseProvider(model.OpencodeGoProvider)
			return err
		},
		"login-opencode-rejected": func(t *testing.T, output *bytes.Buffer) error {
			t.Helper()

			harry := wizard{
				output: output,
				choose: menuChoices(output, 2, 0),
				login: func(chosen provider, presentAddress func(string)) error {
					if chosen.identifier == model.OpencodeGoProvider {
						return storeOpenCodeGoKey(
							strings.NewReader("bad-key\n"),
							output,
							filepath.Join(t.TempDir(), "auth.json"),
							func(string) error { return errors.New("the key was refused") },
						)
					}
					presentAddress("https://example.test/authorise")
					return nil
				},
			}
			err := harry.chooseProvider("")
			return err
		},
		"login-retry": func(_ *testing.T, output *bytes.Buffer) error {
			attempts := 0
			harry := wizard{
				output: output,
				choose: menuChoices(output, 0, 1),
				login: func(_ provider, presentAddress func(string)) error {
					attempts++
					presentAddress("https://example.test/authorise")
					if attempts == 1 {
						return errors.New("authorisation was refused")
					}
					return nil
				},
			}
			err := harry.chooseProvider("")
			return err
		},
		"login-cancelled": func(_ *testing.T, output *bytes.Buffer) error {
			harry := wizard{
				output: output,
				choose: func(prompt string, labels []string) (int, error) {
					if _, err := output.WriteString(picker.RenderMenu(prompt, labels, 0)); err != nil {
						return 0, err
					}
					return 0, ErrCancelled
				},
			}
			err := harry.chooseProvider("")
			if !errors.Is(err, ErrCancelled) {
				return fmt.Errorf("got %w", err)
			}
			return nil
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
			if name == "login-anthropic" || name == "login-browser-failure" || name == "login-long-url" {
				assertANSIGolden(t, name, output.String())
			}
		})
	}
}

func menuChoices(output *bytes.Buffer, choices ...int) func(string, []string) (int, error) {
	choiceIndex := 0
	return func(prompt string, labels []string) (int, error) {
		chosen := choices[choiceIndex]
		choiceIndex++
		_, err := output.WriteString(picker.RenderMenu(prompt, labels, chosen))
		return chosen, err
	}
}

func assertANSIGolden(t *testing.T, name string, rendered string) {
	t.Helper()

	got := strings.NewReplacer("\x1b", `\e`, "\r", `\r`).Replace(rendered)
	path := filepath.Join("testdata", name+".ansi")
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // the test's own golden
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("rendering differs from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

func assertScreenGolden(t *testing.T, name string, rendered string) {
	t.Helper()

	got := visibleTranscript(rendered)
	path := filepath.Join("testdata", name+".screen")
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // the test's own golden
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("rendering differs from %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}
