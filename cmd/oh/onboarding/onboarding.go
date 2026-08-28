package onboarding

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"crdx.org/io/cmd/oh/link"
	"crdx.org/io/cmd/oh/model"
	"crdx.org/io/cmd/oh/picker"
	"crdx.org/io/cmd/oh/tty"
	"crdx.org/io/internal/browser"
	"crdx.org/io/provider/anthropic"
	"crdx.org/io/provider/codex"
	"crdx.org/io/provider/opencodego"
)

// ErrCancelled means the user left the provider picker without signing in.
var ErrCancelled = picker.ErrCancelled

const (
	chatGPTName    = "ChatGPT"
	anthropicName  = "Anthropic"
	openCodeGoName = "OpenCode Go"

	validationModel           = "validation"
	validationEffort          = "none"
	validationMaxOutputTokens = 1
)

type provider struct {
	name       string
	identifier string
}

var providers = []provider{
	{name: chatGPTName, identifier: model.CodexProvider},
	{name: anthropicName, identifier: model.AnthropicProvider},
	{name: openCodeGoName, identifier: model.OpencodeGoProvider},
}

func Login(providerName string, terminal *os.File, output io.Writer) error {
	harry := wizard{
		output: output,
		choose: func(prompt string, labels []string) (int, error) {
			return picker.ChooseIndex(terminal, output, prompt, labels)
		},
		login:       login(terminal, output),
		openBrowser: browser.Open,
	}

	return harry.chooseProvider(providerName)
}

type wizard struct {
	output      io.Writer
	choose      func(string, []string) (int, error)
	login       func(provider, func(string)) error
	openBrowser func(string) error
}

func (self wizard) chooseProvider(providerName string) error {
	if providerName != "" {
		chosenProvider, found := providerNamed(providerName)
		if !found {
			return fmt.Errorf(
				"unknown provider %q: choose one of %s",
				providerName,
				strings.Join(model.LoginProviderNames(), ", "),
			)
		}

		return self.authenticate(chosenProvider, false)
	}

	labels := make([]string, len(providers))
	for i, provider := range providers {
		labels[i] = provider.name
	}

	for {
		chosen, err := self.choose("Choose your provider:", labels)
		if err != nil {
			return err
		}

		if err := self.authenticate(providers[chosen], true); err == nil {
			return nil
		} else if _, writeErr := fmt.Fprintf(self.output, "Couldn’t sign in: %s\n\n", err); writeErr != nil {
			return writeErr
		}
	}
}

func providerNamed(identifier string) (provider, bool) {
	for _, candidate := range providers {
		if candidate.identifier == identifier {
			return candidate, true
		}
	}
	return provider{}, false
}

func (self wizard) authenticate(chosenProvider provider, shouldSeparate bool) error {
	if shouldSeparate {
		if _, err := fmt.Fprintln(self.output); err != nil {
			return err
		}
	}

	err := self.login(chosenProvider, func(address string) {
		_, _ = fmt.Fprintf(self.output, "Opening your browser to authorise %s…\n", chosenProvider.name)
		_, _ = fmt.Fprintln(self.output, link.RenderURL(address, address))
		if self.openBrowser != nil {
			if err := self.openBrowser(address); err != nil {
				_, _ = fmt.Fprintf(self.output, "Could not open a browser: %s\n", err)
				_, _ = fmt.Fprintln(self.output, "Visit the address above to continue.")
			}
		}
		_, _ = fmt.Fprintln(
			self.output,
			"If authorisation does not complete automatically, paste the complete redirect URL here and press Enter.",
		)
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(self.output, "✓ Signed in\n\n")
	return err
}

func login(terminal *os.File, output io.Writer) func(provider, func(string)) error {
	return func(chosenProvider provider, presentAddress func(string)) error {
		switch chosenProvider.identifier {
		case model.CodexProvider:
			return loginWithRedirect(terminal, func(ctx context.Context, redirects <-chan string) error {
				return codex.LoginWithRedirect(ctx, presentAddress, redirects)
			})
		case model.AnthropicProvider:
			return loginWithRedirect(terminal, func(ctx context.Context, redirects <-chan string) error {
				return anthropic.LoginWithRedirect(ctx, presentAddress, redirects)
			})
		case model.OpencodeGoProvider:
			return loginOpenCodeGo(terminal, output)
		default:
			return fmt.Errorf("unknown provider %q", chosenProvider.identifier)
		}
	}
}

func loginWithRedirect(
	input *os.File,
	authenticate func(context.Context, <-chan string) error,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redirects := make(chan string, 1)
	go func() {
		redirect, err := tty.ReadLine(ctx, input)
		if err == nil {
			redirects <- redirect
		}
	}()

	return authenticate(ctx, redirects)
}

func loginOpenCodeGo(terminal *os.File, output io.Writer) error {
	return storeOpenCodeGoKey(terminal, output, opencodego.CredentialsPath(), validateOpenCodeGoKey)
}

func storeOpenCodeGoKey(
	input io.Reader,
	output io.Writer,
	path string,
	validateKey func(string) error,
) error {
	key, err := readOpenCodeGoKey(input, output)
	if err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return fmt.Errorf("validate OpenCode Go API key: %w", err)
	}
	return opencodego.SaveKeyAt(path, key)
}

func validateOpenCodeGoKey(key string) error {
	return validateOpenCodeGoKeyAt(key, opencodego.UsageEndpointURL)
}

func validateOpenCodeGoKeyAt(key string, usageURL string) error {
	client, err := opencodego.New(opencodego.EndpointURL, key, validationModel, validationEffort, validationMaxOutputTokens)
	if err != nil {
		return err
	}
	client.UsageURL = usageURL
	_, err = client.UsageWindows(context.Background())
	return err
}

func readOpenCodeGoKey(input io.Reader, output io.Writer) (string, error) {
	if _, err := fmt.Fprint(output, "OpenCode Go API key: "); err != nil {
		return "", err
	}

	var key string
	if terminal, ok := input.(*os.File); ok && tty.Is(terminal) {
		var err error
		key, err = tty.ReadMaskedLine(terminal, output)
		if err != nil {
			return "", fmt.Errorf("read API key: %w", err)
		}
	} else {
		line, err := bufio.NewReader(input).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read API key: %w", err)
		}
		key = strings.TrimSpace(line)
		if _, err := fmt.Fprintln(output, strings.Repeat("*", len(key))); err != nil {
			return "", err
		}
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("OpenCode Go API key is empty")
	}
	return key, nil
}
