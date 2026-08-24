package snippets_test

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/slash"
	"crdx.org/io/cmd/oh/snippets"
)

type snippetContext struct {
	sent string
}

func (self *snippetContext) Emit(agent.Event) {}
func (self *snippetContext) Send(prompt string) {
	self.sent = prompt
}
func (self *snippetContext) Notice(string)  {}
func (self *snippetContext) Success(string) {}

func TestSnippetSendsItsConfiguredPrompt(t *testing.T) {
	invocation := getInvocation(t, map[string]string{"review": "  Review the changes.  "}, "//review now")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if context.sent != "Review the changes." {
		t.Errorf("sent %q", context.sent)
	}
}

func TestSnippetRendersArgumentsWithGoTemplates(t *testing.T) {
	configured := map[string]string{
		"add": `{{- range $i, $argument := .Args }}{{if $i}}, {{end}}{{ $argument | printf "%q" }}{{end}}: {{.Arg}}`,
	}
	invocation := getInvocation(t, configured, "//add first second")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if context.sent != `"first", "second": first second` {
		t.Errorf("sent %q", context.sent)
	}
}

func TestSnippetArgumentsAreRequiredWithoutADefault(t *testing.T) {
	invocation := getInvocation(t, map[string]string{"review": "Review: {{.Arg}}"}, "//review")

	err := invocation.Command.Run(&snippetContext{}, invocation.Arguments)
	if !slash.IsUsageError(err) {
		t.Errorf("got error %v", err)
	}
	if got := slash.FormatError(invocation, err); got != "Usage: //review <args>" {
		t.Errorf("got formatted error %q", got)
	}
}

func TestSnippetDefaultAllowsMissingArguments(t *testing.T) {
	configured := map[string]string{
		"review": `{{define "value"}}{{ . | default "the current changes" }}{{end}}Review {{template "value" .Arg}}.`,
	}
	invocation := getInvocation(t, configured, "//review")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if context.sent != "Review the current changes." {
		t.Errorf("sent %q", context.sent)
	}
}

func TestSnippetTemplateErrorsAreReported(t *testing.T) {
	tests := map[string]struct {
		configured map[string]string
		input      string
		want       string
	}{
		"parse": {
			configured: map[string]string{"review": "{{"},
			want:       "review",
		},
		"execute": {
			configured: map[string]string{"review": "{{index .Args 2}}"},
			input:      "//review only-one",
			want:       "could not render template",
		},
		"empty result": {
			configured: map[string]string{"review": `{{default "" .Arg}}`},
			input:      "//review",
			want:       "empty prompt",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			set, err := snippets.New(test.configured)
			if test.input == "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Errorf("got error %v, want %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			registry, err := slash.NewRegistry(set)
			if err != nil {
				t.Fatal(err)
			}
			invocation, found := registry.Find(test.input)
			if !found {
				t.Fatalf("expected %q to be registered", test.input)
			}
			context := &snippetContext{}
			err = invocation.Command.Run(context, invocation.Arguments)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("got error %v, want %q", err, test.want)
			}
			if context.sent != "" {
				t.Errorf("sent prompt %q after an error", context.sent)
			}
		})
	}
}

func TestSnippetDefinitionsAreValidated(t *testing.T) {
	for name, configured := range map[string]map[string]string{
		"empty name":        {"": "Prompt"},
		"spaced name":       {"code review": "Prompt"},
		"slash in name":     {"review/code": "Prompt"},
		"prefixed name":     {"//review": "Prompt"},
		"empty prompt":      {"review": ""},
		"whitespace prompt": {"review": "  \t  "},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := snippets.New(configured); err == nil {
				t.Error("expected invalid snippet to return an error")
			}
		})
	}
}

func TestSnippetUsagesAreDeterministic(t *testing.T) {
	set, err := snippets.New(map[string]string{
		"test":     "Run tests.",
		"review":   "Review changes.",
		"optional": `{{.Arg | default "Use the current context."}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"//optional", "//review <args>", "//test <args>"}
	if got := set.Usages(); !slices.Equal(got, want) {
		t.Errorf("got usages %v, want %v", got, want)
	}
}

func TestAnEmptySnippetSetStillOwnsItsPrefix(t *testing.T) {
	set, err := snippets.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Usages()) != 0 {
		t.Errorf("got usages %v", set.Usages())
	}
	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	name, found := registry.CommandName("//missing")
	if !found || name != "//missing" {
		t.Errorf("got command name %q and %t", name, found)
	}
}

func getInvocation(t *testing.T, configured map[string]string, input string) slash.Invocation {
	t.Helper()

	set, err := snippets.New(configured)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	invocation, found := registry.Find(input)
	if !found {
		t.Fatalf("expected %q to be registered", input)
	}
	return invocation
}
