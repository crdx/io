package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/workspaceDir"
	"crdx.org/io/cmd/oh/snippets"
	"crdx.org/io/cmd/oh/work"
)

func TestConfiguredSkillDirectoriesResolvesAbsoluteRelativeAndHomePaths(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.toml")
	absolute := filepath.Join(t.TempDir(), "skills")
	home := t.TempDir()
	t.Setenv("HOME", home)
	contents := "[input]\ncontinue = \"carry on\"\n[model]\nround_robin = [\"opencode/deepseek@hi\"]\n[editor]\ncommand = \"  subl  \"\n[skills]\ninclude = [\"" + absolute + "\", \"shared/skills\", \"~/.system/config/pi/agent/skills\"]\n"
	if err := writeConfigFile(path, contents); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(config.Model.RoundRobin, []string{"opencode/deepseek@hi"}) {
		t.Errorf("got model rotation %#v", config.Model.RoundRobin)
	}
	if !slices.Equal(config.Editor.Command, []string{"subl"}) {
		t.Errorf("got editor %q", config.Editor.Command)
	}
	if config.Input.Continue != "carry on" {
		t.Errorf("got continue message %q", config.Input.Continue)
	}
	directories := config.Skills.Include
	want := []string{
		absolute,
		filepath.Join(configDir, "shared", "skills"),
		filepath.Join(home, ".system", "config", "pi", "agent", "skills"),
	}
	if len(directories) != len(want) {
		t.Fatalf("got %#v, want %#v", directories, want)
	}
	for i := range want {
		if directories[i] != want[i] {
			t.Errorf("directory %d is %q, want %q", i, directories[i], want[i])
		}
	}
}

func TestConfiguredDefaultCapabilitiesRejectUnknownFlags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[caps]\ndefault = \"rwz\"\n"); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown capability flag") {
		t.Errorf("got error %v", err)
	}
}

func TestConfiguredOllamaHostIsTrimmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[provider.ollama]\nhost = \"  speeder:11434  \"\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider.Ollama.Host != "speeder:11434" {
		t.Errorf("got Ollama host %q", config.Provider.Ollama.Host)
	}
}

func TestConfiguredOllamaHostCannotBeEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[provider.ollama]\nhost = \"  \"\n"); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "provider.ollama.host is empty") {
		t.Fatalf("got error %v", err)
	}
}

func TestConfiguredHostnameNamesTheSession(t *testing.T) {
	settings := configFrom(t, `
		[ports]
		hostname = "  preview-{session}.agent  "
	`)

	if got := settings.Ports.GetHostname("brave-otter", "127.27.192.223"); got != "preview-brave-otter.agent" {
		t.Errorf("got %q", got)
	}
}

func TestTheDefaultHostnameIsTheSessionAddress(t *testing.T) {
	settings := configFrom(t, "")

	if got := settings.Ports.GetHostname("brave-otter", "127.27.192.223"); got != "127.27.192.223" {
		t.Errorf("got %q", got)
	}
}

func TestConfiguredHostnameNeedsExactlyOneSessionPlaceholder(t *testing.T) {
	for name, hostname := range map[string]string{
		"missing":  "preview.agent",
		"repeated": "{session}.{session}.agent",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			contents := "[ports]\nhostname = \"" + hostname + "\"\n"
			if err := writeConfigFile(path, contents); err != nil {
				t.Fatal(err)
			}

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "ports.hostname must contain {session} exactly once") {
				t.Errorf("got error %v", err)
			}
		})
	}
}

func TestConfiguredEditorAcceptsArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[editor]\ncommand = [\"subl\", \"--wait\"]\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(config.Editor.Command, []string{"subl", "--wait"}) {
		t.Errorf("got editor %q", config.Editor.Command)
	}
}

func TestAMissingConfigFileIsAllowed(t *testing.T) {
	config, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Skills.Include) != 0 || len(config.Sandbox.Read) != 0 ||
		len(config.Sandbox.Write) != 0 || len(config.Sandbox.Exec) != 0 {
		t.Errorf("got %#v, want no configured paths", config)
	}
	if len(config.Editor.Command) != 0 {
		t.Errorf("got default editor %q", config.Editor.Command)
	}
	if config.Input.Continue != "yes" {
		t.Errorf("got default continue message %q", config.Input.Continue)
	}
	if got := caps.Set(config.Caps.Default).Flags(); got != "rx" {
		t.Errorf("got default capabilities %q", got)
	}
	if config.Version != Format {
		t.Errorf("got config format %d, want %d", config.Version, Format)
	}
}

func TestAnUnversionedConfigNeedsMigrating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("model = \"gpt\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "ohctl migrate") {
		t.Fatalf("expected migration instructions, got %v", err)
	}
}

func TestAnUnversionedOverrideReplacesOnlyWhatItMentions(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "global", "config.toml")
	if err := os.Mkdir(filepath.Dir(globalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigFile(globalPath, "[input]\ncontinue = \"globally\"\n[model]\nround_robin = [\"anthropic/global\"]\n[sandbox]\nread = [\"shared\"]\n"); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "project", "oh.toml")
	if err := os.Mkdir(filepath.Dir(overridePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overridePath, []byte("[caps]\ndefault = \"rwg\"\n[input]\ncontinue = \"locally\"\n[sandbox]\nwrite = [\"output\"]\n[snippets]\nreview = { prompt = \"Review.\", arguments = \"none\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Input.Continue != "locally" {
		t.Errorf("got continue message %q", settings.Input.Continue)
	}
	if got := caps.Set(settings.Caps.Default).Flags(); got != "rwg" {
		t.Errorf("got default capabilities %q", got)
	}
	if !slices.Equal(settings.Model.RoundRobin, []string{"anthropic/global"}) {
		t.Errorf("got model rotation %#v", settings.Model.RoundRobin)
	}
	if !slices.Equal(settings.Sandbox.Read, []string{filepath.Join(filepath.Dir(globalPath), "shared")}) {
		t.Errorf("got read paths %#v", settings.Sandbox.Read)
	}
	if !slices.Equal(settings.Sandbox.Write, []string{filepath.Join(filepath.Dir(overridePath), "output")}) {
		t.Errorf("got write paths %#v", settings.Sandbox.Write)
	}
	override, exists := settings.GetOverride()
	if !exists {
		t.Fatal("local override was not reported")
	}
	if override.Path != overridePath {
		t.Errorf("got override path %q, want %q", override.Path, overridePath)
	}
	wantSettings := []string{"caps.default", "input.continue", "sandbox.write", "snippets.review"}
	if !slices.Equal(override.Settings, wantSettings) {
		t.Errorf("got overridden settings %#v, want %#v", override.Settings, wantSettings)
	}
}

func TestAnOverrideMergesAdditiveListsInSourceOrder(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	globalDirectory := filepath.Join(directory, "global")
	localDirectory := filepath.Join(directory, "local")
	if err := os.MkdirAll(globalDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	sharedPath := filepath.Join(directory, "shared")
	globalPath := filepath.Join(globalDirectory, "config.toml")
	globalBody := fmt.Sprintf("[skills]\ninclude = [\"global-skills\", %q]\nexclude = [\"global-excluded\"]\n[sandbox]\nhost_loopback = [1111, 2222]\nread = [\"global-read\", %q]\nwrite = [\"global-write\"]\nexec = [\"global-exec\"]\nhome = [\"global-home\"]\n", sharedPath, sharedPath)
	if err := writeConfigFile(globalPath, globalBody); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(localDirectory, "oh.toml")
	localBody := fmt.Sprintf("[skills]\ninclude = [\"local-skills\", %q]\nexclude = [\"local-excluded\"]\n[sandbox]\nhost_loopback = [2222, 3333]\nread = [\"local-read\", %q]\nwrite = [\"local-write\"]\nexec = [\"local-exec\"]\nhome = [\"local-home\"]\n", sharedPath, sharedPath)
	if err := os.WriteFile(localPath, []byte(localBody), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSources(
		Source{Path: globalPath},
		Source{Path: localPath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		got  []string
		want []string
	}{
		{"skills.include", settings.Skills.Include, []string{filepath.Join(globalDirectory, "global-skills"), sharedPath, filepath.Join(localDirectory, "local-skills")}},
		{"skills.exclude", settings.Skills.Exclude, []string{filepath.Join(globalDirectory, "global-excluded"), filepath.Join(localDirectory, "local-excluded")}},
		{"sandbox.read", settings.Sandbox.Read, []string{filepath.Join(globalDirectory, "global-read"), sharedPath, filepath.Join(localDirectory, "local-read")}},
		{"sandbox.write", settings.Sandbox.Write, []string{filepath.Join(globalDirectory, "global-write"), filepath.Join(localDirectory, "local-write")}},
		{"sandbox.exec", settings.Sandbox.Exec, []string{filepath.Join(globalDirectory, "global-exec"), filepath.Join(localDirectory, "local-exec")}},
		{"sandbox.home", settings.Sandbox.Home, []string{filepath.Join(globalDirectory, "global-home"), filepath.Join(localDirectory, "local-home")}},
	}
	for _, check := range checks {
		if !slices.Equal(check.got, check.want) {
			t.Errorf("%s = %#v, want %#v", check.name, check.got, check.want)
		}
	}
	if want := []uint16{1111, 2222, 3333}; !slices.Equal(settings.Sandbox.HostLoopback, want) {
		t.Errorf("sandbox.host_loopback = %#v, want %#v", settings.Sandbox.HostLoopback, want)
	}
}

func TestAnEmptyLocalAdditiveListKeepsTheGlobalEntries(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[skills]\ninclude = [\"global-skills\"]\n"); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(localPath, []byte("[skills]\ninclude = []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSources(
		Source{Path: globalPath},
		Source{Path: localPath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(directory, "global-skills")}
	if !slices.Equal(settings.Skills.Include, want) {
		t.Errorf("skills.include = %#v, want %#v", settings.Skills.Include, want)
	}
}

func TestAMissingOverrideKeepsTheGlobalConfig(t *testing.T) {
	globalPath := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(globalPath, "[input]\ncontinue = \"globally\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSources(
		Source{Path: globalPath},
		Source{Path: filepath.Join(t.TempDir(), "oh.toml"), IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Input.Continue != "globally" {
		t.Errorf("got continue message %q", settings.Input.Continue)
	}
	if override, exists := settings.GetOverride(); exists {
		t.Errorf("missing override was reported as %#v", override)
	}
}

func TestAnUnknownOverrideSettingNamesTheLocalFile(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[input]\ncontinue = \"globally\"\n"); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[input]\nmystery = \"locally\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	report := strings.Join(settings.UnknownSettings(), "\n")
	if !strings.Contains(report, overridePath) || !strings.Contains(report, "input.mystery") {
		t.Errorf("got report %q", report)
	}
}

func TestAMalformedOverrideNamesTheLocalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oh.toml")
	if err := os.WriteFile(path, []byte("not toml = ["), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSources(Source{Path: path, IsOverride: true})
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("got error %v", err)
	}
}

func TestAnEmptyOverrideIsReportedWithNoSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oh.toml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSources(Source{Path: path, IsOverride: true})
	if err != nil {
		t.Fatal(err)
	}
	override, exists := settings.GetOverride()
	if !exists {
		t.Fatal("empty override was not reported")
	}
	if override.Path != path || len(override.Settings) != 0 {
		t.Errorf("got override %#v", override)
	}
}

func TestAConfigFromANewerOhIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("version = %d\n", Format+1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "upgrade oh") {
		t.Fatalf("expected the newer format to be refused, got %v", err)
	}
}

func TestAConfigFromANewerOhIsRefusedBeforeItsShapeIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("version = %d\nmodel = \"gpt\"\n", Format+1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "upgrade oh") {
		t.Fatalf("expected the newer format to be refused, got %v", err)
	}
	if strings.Contains(err.Error(), "incompatible types") {
		t.Errorf("expected the decoder complaint to be replaced by the format, got %v", err)
	}
}

func TestConfiguredSkillDirectoriesRejectsAnEmptyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[skills]\ninclude = [\"\"]\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Error("expected an empty skill directory to be rejected")
	}
}

func TestTheSingularSkillTableIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[skill]\ninclude = [\"skills\"]\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	report := strings.Join(config.UnknownSettings(), "\n")
	if !strings.Contains(report, "skill.include") {
		t.Errorf("expected the singular skill table to be reported, got %q", report)
	}
}

func TestConfiguredSkillExclusionsResolveToAbsoluteDirectories(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.toml")
	if err := writeConfigFile(path, "[skills]\nexclude = [\"skills/pi\"]\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configDir, "skills", "pi")
	if len(config.Skills.Exclude) != 1 || config.Skills.Exclude[0] != want {
		t.Errorf("got exclusions %#v, want [%s]", config.Skills.Exclude, want)
	}
}

func TestConfiguredSkillExclusionsRejectAnEmptyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[skills]\nexclude = [\"\"]\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Error("expected an empty skill directory to be rejected")
	}
}

func TestConfiguredStringsCannotBeEmpty(t *testing.T) {
	for name, contents := range map[string]string{
		"model round robin":          "[model]\nround_robin = []\n",
		"model selection":            "[model]\nround_robin = [\"\"]\n",
		"model selection whitespace": "[model]\nround_robin = [\"  \"]\n",
		"input continue":             "[input]\ncontinue = \"\"\n",
		"input continue whitespace":  "[input]\ncontinue = \"  \"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := writeConfigFile(path, contents); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Errorf("expected empty %s to be rejected", name)
			}
		})
	}
}

func TestTheConfiguredContinueMessageIsTrimmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[input]\ncontinue = \"  carry on  \"\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Input.Continue != "carry on" {
		t.Errorf("got continue message %q", config.Input.Continue)
	}
}

func TestAStringSnippetIsItsPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[snippets]\ngold = \"  Consider the goldens.  \"\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	definition := config.Snippets["gold"]
	if definition.Prompt != "Consider the goldens." || definition.File != "" ||
		definition.Description != "" || definition.Arguments != "" {
		t.Errorf("got snippet %#v", definition)
	}
}

func TestAnEmptyStringSnippetIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[snippets]\ngold = \"  \"\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "snippets.gold") {
		t.Errorf("got error %v", err)
	}
}

func TestANonStringNonTableSnippetIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[snippets]\ngold = 5\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "not a prompt or a table") {
		t.Errorf("got error %v", err)
	}
}

func TestRichConfiguredSnippetsAreLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := `[snippets]
review = { prompt = "  Review {{ .Arg }}.  ", description = "  Review changes.  ", arguments = "optional" }
`
	if err := writeConfigFile(path, contents); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	definition := config.Snippets["review"]
	if definition.Prompt != "Review {{ .Arg }}." || definition.Description != "Review changes." ||
		definition.Arguments != snippets.ArgumentsOptional {
		t.Errorf("got snippet %#v", definition)
	}
}

func TestSnippetDescriptionAndArgumentsAreOptional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[snippets]\nreview = { prompt = \"Review {{ .Arg }}.\" }\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	definition := config.Snippets["review"]
	if definition.Description != "" || definition.Arguments != "" {
		t.Errorf("got snippet %#v", definition)
	}
}

func TestSnippetPromptFilesAreRelativeToTheConfig(t *testing.T) {
	configDirectory := t.TempDir()
	promptDirectory := filepath.Join(configDirectory, "snippets")
	if err := os.Mkdir(promptDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(promptDirectory, "review.md")
	if err := os.WriteFile(promptPath, []byte("  Review {{ .Arg }}.  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDirectory, "config.toml")
	contents := `[snippets]
review = { file = "snippets/review.md", description = "Review changes.", arguments = "required" }
`
	if err := writeConfigFile(configPath, contents); err != nil {
		t.Fatal(err)
	}

	config, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	definition := config.Snippets["review"]
	if definition.Prompt != "Review {{ .Arg }}." || definition.File != promptPath {
		t.Errorf("got snippet %#v", definition)
	}
}

func TestAMissingSnippetPromptFileIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := `[snippets]
review = { file = "missing.md", description = "Review changes.", arguments = "required" }
`
	if err := writeConfigFile(path, contents); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "snippets.review") {
		t.Errorf("got error %v", err)
	}
}

func TestRichSnippetDefinitionsAreValidated(t *testing.T) {
	for name, definition := range map[string]string{
		"both sources":          `{ prompt = "Prompt", file = "prompt.md", description = "Description", arguments = "none" }`,
		"missing source":        `{ description = "Description", arguments = "none" }`,
		"multiline description": "{ prompt = \"Prompt\", description = \"\"\"\nfirst\nsecond\n\"\"\", arguments = \"none\" }",
		"invalid arguments":     `{ prompt = "Prompt", description = "Description", arguments = "sometimes" }`,
		"unknown field":         `{ prompt = "Prompt", description = "Description", arguments = "none", extra = "x" }`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := writeConfigFile(path, "[snippets]\nreview = "+definition+"\n"); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "snippets.review") {
				t.Errorf("got error %v", err)
			}
		})
	}
}

func TestConfiguredSnippetPromptsCannotBeEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := `[snippets]
review = { prompt = "  ", description = "Review changes.", arguments = "none" }
`
	if err := writeConfigFile(path, contents); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "snippets.review") {
		t.Errorf("expected snippets.review error, got %v", err)
	}
}

func TestConfiguredAccessPathsAreResolved(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.toml")
	home := t.TempDir()
	t.Setenv("HOME", home)
	contents := "[sandbox]\nread = [\"~/reference\"]\nwrite = [\"output\"]\nexec = [\"/opt/tools\"]\n" +
		"home = [\"~/.gitconfig\"]\n"
	if err := writeConfigFile(path, contents); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	assertPaths := func(name string, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s got %#v, want %#v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s path %d is %q, want %q", name, i, got[i], want[i])
			}
		}
	}
	assertPaths("read", config.Sandbox.Read, []string{filepath.Join(home, "reference")})
	assertPaths("write", config.Sandbox.Write, []string{filepath.Join(configDir, "output")})
	assertPaths("exec", config.Sandbox.Exec, []string{"/opt/tools"})
	assertPaths("home", config.Sandbox.Home, []string{filepath.Join(home, ".gitconfig")})
}

func TestHostLoopbackPortsAreLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[sandbox]\nhost_loopback = [80, 3000]\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(config.Sandbox.HostLoopback, []uint16{80, 3000}) {
		t.Errorf("got ports %v", config.Sandbox.HostLoopback)
	}
}

func TestHostLoopbackPortsMustBeNonzeroAndUnique(t *testing.T) {
	for _, ports := range []string{"[0]", "[80, 80]"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := writeConfigFile(path, "[sandbox]\nhost_loopback = "+ports+"\n"); err != nil {
			t.Fatal(err)
		}

		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "sandbox.host_loopback") {
			t.Errorf("got %v for %s", err, ports)
		}
	}
}

func TestAPathMappedIntoTheShellHomeMustComeFromTheHomeDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("HOME", t.TempDir())
	if err := writeConfigFile(path, "[sandbox]\nhome = [\"/etc/gitconfig\"]\n"); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "sandbox.home") {
		t.Errorf("the error does not name the setting: %v", err)
	}
}

func TestTheBuiltInBarLayoutCanBeBuilt(t *testing.T) {
	layoutFrom(t, "")
}

func TestFastModeCanBePlacedIndependently(t *testing.T) {
	layout := layoutFrom(t, `
		[bar.top]
		center = [{ segment = "fast-mode" }]
	`)

	if got := len(layout[segment.TopCenter]); got != 1 {
		t.Errorf("got %d segments", got)
	}
	instance, isNamed := layout[segment.TopCenter][0].(segment.Instance)
	if !isNamed || instance.Name != "fast-mode" {
		t.Errorf("built segment lost its configured name: %#v", layout[segment.TopCenter][0])
	}
}

func TestWhatAConfigDoesNotMentionKeepsItsDefault(t *testing.T) {
	defaults := layoutFrom(t, "")
	layout := layoutFrom(t, `
		[bar.bottom]
		left = [{ segment = "workspace-dir" }, { segment = "mode-toggle" }]
	`)

	if got := len(layout[segment.BottomLeft]); got != 2 {
		t.Errorf("expected what the file said, got %d segments", got)
	}

	for _, position := range segment.Positions {
		if position == segment.BottomLeft {
			continue
		}

		if got, want := len(layout[position]), len(defaults[position]); got != want {
			t.Errorf("expected the default at %s to have %d segments, got %d", position, want, got)
		}
	}
}

func TestAnOverrideCanReplaceAGlobalBarPosition(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[bar.top]\ncenter = [{ segment = \"scroll-overflow\", direction = \"up\" }]\n"); err != nil {
		t.Fatal(err)
	}
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[bar.top]\ncenter = [{ segment = \"workspace-dir\", type = \"base\" }]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := LoadSources(
		Source{Path: globalPath},
		Source{Path: overridePath, IsOverride: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	layout, err := settings.BuildLayout(testSegments())
	if err != nil {
		t.Fatal(err)
	}
	if reports := settings.UnknownSettings(); len(reports) > 0 {
		t.Fatal(reports)
	}
	if got := len(layout[segment.TopCenter]); got != 1 {
		t.Errorf("got %d segments", got)
	}
	override, exists := settings.GetOverride()
	if !exists || !slices.Equal(override.Settings, []string{"bar.top.center"}) {
		t.Errorf("got override %#v, exists=%t", override, exists)
	}
}

func TestAnEmptyListClearsWhatTheDefaultPutThere(t *testing.T) {
	layout := layoutFrom(t, "[bar.top]\nright = []\n")

	if got := len(layout[segment.TopRight]); got != 0 {
		t.Errorf("expected the rule to be cleared, got %d segments", got)
	}
}

func TestAPlacementNamingASegmentThatIsNotOfferedSaysWhereAndWhatInstead(t *testing.T) {
	_, err := brokenLayout(t, `
		[bar.top]
		center = [{ segment = "weather" }]
	`)
	if err == nil {
		t.Fatal("expected an unknown segment to be refused")
	}

	for _, want := range []string{"top.center", "weather", "activity"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q to mention %q", err, want)
		}
	}
}

func TestAPlacementGivenOptionsItsSegmentRefusesIsRefused(t *testing.T) {
	_, err := brokenLayout(t, `
		[bar.top]
		center = [{ segment = "scroll-overflow", direction = "sideways" }]
	`)
	if err == nil {
		t.Fatal("expected a bad direction to be refused")
	}

	for _, want := range []string{"top.center", "scroll", "sideways"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q to mention %q", err, want)
		}
	}
}

func TestAPlacementSettingWhatItsSegmentDoesNotReadIsReported(t *testing.T) {
	config := configFrom(t, `
		[bar.top]
		center = [{ segment = "workspace-dir", loudly = true }]
	`)

	if _, err := config.BuildLayout(testSegments()); err != nil {
		t.Fatal(err)
	}

	reports := config.UnknownSettings()
	if len(reports) == 0 {
		t.Fatal("expected a setting nothing reads to be reported")
	}
	if !strings.Contains(strings.Join(reports, "\n"), "loudly") {
		t.Errorf("expected %q to name the setting", reports)
	}
}

func TestTheBuiltInDefaultsSetEverySettingThereIs(t *testing.T) {
	var written map[string]any
	if _, err := toml.Decode(defaultsTOML, &written); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"version", "caps", "editor", "input", "model", "snippets", "skills", "sandbox", "bar",
	} {
		if _, ok := written[key]; !ok {
			t.Errorf("expected the defaults to say what %q is", key)
		}
	}
}

type inertSegment struct{}

func (inertSegment) Render(segment.Context) string {
	return ""
}

func inertFactory(segment.Options) (segment.Segment, error) {
	return inertSegment{}, nil
}

func testSegments() segment.Registry {
	return segment.Registry{
		"activity-spinner":   inertFactory,
		"cache-usage":        inertFactory,
		"context-usage":      inertFactory,
		"fast-mode":          inertFactory,
		"mode-toggle":        inertFactory,
		"path-grants":        inertFactory,
		"exposed-ports":      inertFactory,
		"workspace-dir":      workspaceDir.New(work.At("/tmp/somewhere")),
		"active-model":       inertFactory,
		"scroll-overflow":    scrollOverflow.New,
		"session-name":       inertFactory,
		"session-spend":      inertFactory,
		"local-time":         inertFactory,
		"turn-timer":         inertFactory,
		"turn-count":         inertFactory,
		"git-branch":         inertFactory,
		"jobs":               inertFactory,
		"subscription-usage": inertFactory,
	}
}

func writeConfigFile(path string, body string) error {
	version := fmt.Sprintf("version = %d\n", Format)

	return os.WriteFile(path, []byte(version+body), 0o600)
}

func configFrom(t *testing.T, body string) Config {
	t.Helper()

	if body == "" {
		config, err := Load("")
		if err != nil {
			t.Fatal(err)
		}

		return config
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, undent(body)); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return config
}

func undent(body string) string {
	var out strings.Builder
	for row := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		out.WriteString(strings.TrimLeft(row, "\t"))
		out.WriteString("\n")
	}

	return out.String()
}

func layoutFrom(t *testing.T, body string) segment.Layout {
	t.Helper()

	layout, err := configFrom(t, body).BuildLayout(testSegments())
	if err != nil {
		t.Fatal(err)
	}

	return layout
}

func brokenLayout(t *testing.T, body string) (segment.Layout, error) {
	t.Helper()

	return configFrom(t, body).BuildLayout(testSegments())
}

func TestEveryStreamingModeIsAccepted(t *testing.T) {
	for name, want := range map[string]output.StreamingMode{
		"asap":  output.StreamingModeASAP,
		"line":  output.StreamingModeLine,
		"paced": output.StreamingModePaced,
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := writeConfigFile(path, "[ui]\nstreaming = \""+name+"\"\n"); err != nil {
			t.Fatal(err)
		}

		config, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if config.Ui.StreamingMode != want {
			t.Errorf("streaming = %q read as %d, want %d", name, config.Ui.StreamingMode, want)
		}
	}
}

func TestAnUnknownStreamingModeNamesTheOnesThatExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\nstreaming = \"instant\"\n"); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an unknown streaming mode to be refused")
	}
	for _, name := range []string{"instant", "asap", "line", "paced"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("expected %q to be named, got %v", name, err)
		}
	}
}

func TestAStreamingModeThatIsNotTextIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\nstreaming = 3\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected a streaming mode that is not text to be refused")
	}
}

func TestTheStreamingModeDefaultsToWholeLines(t *testing.T) {
	config, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if config.Ui.StreamingMode != output.StreamingModeLine {
		t.Errorf("got streaming mode %d, want whole lines", config.Ui.StreamingMode)
	}
}

func TestAConfigWrittenBeforeTheStreamingModeExistedNeedsNoMigrating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 10\n[input]\ncontinue = \"go on\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatalf("a config from before the setting existed was refused: %v", err)
	}
	if config.Ui.StreamingMode != output.StreamingModeLine {
		t.Errorf("got streaming mode %d, want whole lines", config.Ui.StreamingMode)
	}
	if reports := config.UnknownSettings(); len(reports) > 0 {
		t.Errorf("got %v", reports)
	}
}

func TestTheToolOutputLimitDefaultsToTwelveKilobytes(t *testing.T) {
	config, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if config.Tool.Output.Bytes != 12*1024 {
		t.Errorf("got tool output limit %d, want %d", config.Tool.Output.Bytes, 12*1024)
	}
}

func TestTheToolOutputLimitIsRead(t *testing.T) {
	config := configFrom(t, `
		[tool]
		output = "48K"
	`)

	if config.Tool.Output.Bytes != 48*1024 {
		t.Errorf("got tool output limit %d, want %d", config.Tool.Output.Bytes, 48*1024)
	}
	if reports := config.UnknownSettings(); len(reports) > 0 {
		t.Errorf("got %v", reports)
	}
}

func TestAToolOutputLimitTooSmallToSayAnythingWithIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[tool]\noutput = \"3\"\n"); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "tool.output is too small") {
		t.Errorf("got %v, want a complaint about tool.output", err)
	}
}

func TestAToolOutputLimitThatIsNotASizeIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[tool]\noutput = \"a bit\"\n"); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "is not a size") {
		t.Errorf("got %v, want a complaint about the size", err)
	}
}

func TestAConfigWrittenBeforeTheToolOutputLimitExistedNeedsNoMigrating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 10\n[input]\ncontinue = \"go on\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatalf("a config from before the setting existed was refused: %v", err)
	}
	if config.Tool.Output.Bytes != 12*1024 {
		t.Errorf("got tool output limit %d, want the default %d", config.Tool.Output.Bytes, 12*1024)
	}
}

func TestASizeIsReadPlainlyOrWithAUnitAfterIt(t *testing.T) {
	sizes := []struct {
		written string
		want    int
	}{
		{"0", 0},
		{"512", 512},
		{"12K", 12 * 1024},
		{" 12k ", 12 * 1024},
		{"2M", 2 * 1024 * 1024},
		{"1G", 1024 * 1024 * 1024},
	}

	for _, size := range sizes {
		var read Size
		if err := read.UnmarshalText([]byte(size.written)); err != nil {
			t.Errorf("%q was refused: %v", size.written, err)
		} else if read.Bytes != size.want {
			t.Errorf("%q read as %d, want %d", size.written, read.Bytes, size.want)
		}
	}
}

func TestASizeThatIsNotOneIsRefused(t *testing.T) {
	for _, written := range []string{"", "K", "-1", "12KB", "one", "1.5M"} {
		var size Size
		if err := size.UnmarshalText([]byte(written)); err == nil {
			t.Errorf("%q was read as %d, want a complaint", written, size.Bytes)
		}
	}
}

func TestTheGroupingIsReadAsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ngrouping = [\"notice\", \"reasoning\", \"tool\", \"answer\"]\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	want, err := output.ParseGrouping([]string{"notice", "reasoning", "tool", "answer"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Ui.Grouping, want) {
		t.Errorf("got grouping %+v, want %+v", config.Ui.Grouping, want)
	}
}

func TestAnUnknownGroupIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ngrouping = [\"notice\", \"thinking\", \"answer\"]\n"); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an unknown group to be refused")
	}
	for _, name := range []string{"thinking", "reasoning", "tool"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("expected %q to be named, got %v", name, err)
		}
	}
}

func TestAGroupingThatIsNotAListIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ngrouping = \"notice, tool\"\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected a grouping that is not a list to be refused")
	}
}

func TestAGroupThatIsNotTextIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\ngrouping = [\"notice\", 3]\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("expected a group that is not text to be refused")
	}
}

func TestTheGroupingDefaultsToReasoningRunningOnFromTools(t *testing.T) {
	config, err := Load("")
	if err != nil {
		t.Fatal(err)
	}

	want, err := output.ParseGrouping(output.DefaultGroups)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Ui.Grouping, want) {
		t.Errorf("got grouping %+v, want %+v", config.Ui.Grouping, want)
	}
}

func TestAConfigWrittenBeforeTheGroupingExistedNeedsNoMigrating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("version = 10\n[input]\ncontinue = \"go on\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatalf("a config from before the setting existed was refused: %v", err)
	}

	want, err := output.ParseGrouping(output.DefaultGroups)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Ui.Grouping, want) {
		t.Errorf("got grouping %+v, want %+v", config.Ui.Grouping, want)
	}
	if reports := config.UnknownSettings(); len(reports) > 0 {
		t.Errorf("got %v", reports)
	}
}
