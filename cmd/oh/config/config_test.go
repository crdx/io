package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/workspaceDir"
	"crdx.org/io/cmd/oh/snippets"
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

func TestTheSingularSkillTableIsNotAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[skill]\ninclude = [\"skills\"]\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.ValidateConsumed(); err == nil || !strings.Contains(err.Error(), "skill.include") {
		t.Errorf("expected singular skill table error, got %v", err)
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

func TestAPlacementSettingWhatItsSegmentDoesNotReadIsRefused(t *testing.T) {
	config := configFrom(t, `
		[bar.top]
		center = [{ segment = "workspace-dir", loudly = true }]
	`)

	if _, err := config.BuildLayout(testSegments()); err != nil {
		t.Fatal(err)
	}

	err := config.ValidateConsumed()
	if err == nil {
		t.Fatal("expected a setting nothing reads to be refused")
	}
	if !strings.Contains(err.Error(), "loudly") {
		t.Errorf("expected %q to name the setting", err)
	}
}

func TestTheBuiltInDefaultsSetEverySettingThereIs(t *testing.T) {
	var written map[string]any
	if _, err := toml.Decode(defaultsTOML, &written); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"version", "editor", "input", "model", "snippets", "skills", "sandbox", "bar",
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
		"context-usage":      inertFactory,
		"mode-toggle":        inertFactory,
		"workspace-dir":      workspaceDir.New("/tmp/somewhere"),
		"active-model":       inertFactory,
		"scroll-overflow":    scrollOverflow.New,
		"session-name":       inertFactory,
		"local-time":         inertFactory,
		"turn-timer":         inertFactory,
		"turn-count":         inertFactory,
		"git-branch":         inertFactory,
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
		if err := writeConfigFile(path, "[ui]\nstream = \""+name+"\"\n"); err != nil {
			t.Fatal(err)
		}

		config, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if config.Ui.StreamingMode != want {
			t.Errorf("stream = %q read as %d, want %d", name, config.Ui.StreamingMode, want)
		}
	}
}

func TestAnUnknownStreamingModeNamesTheOnesThatExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[ui]\nstream = \"instant\"\n"); err != nil {
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
	if err := writeConfigFile(path, "[ui]\nstream = 3\n"); err != nil {
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
	if err := os.WriteFile(path, []byte("version = 9\n[input]\ncontinue = \"go on\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatalf("a config from before the setting existed was refused: %v", err)
	}
	if config.Ui.StreamingMode != output.StreamingModeLine {
		t.Errorf("got streaming mode %d, want whole lines", config.Ui.StreamingMode)
	}
	if err := config.ValidateConsumed(); err != nil {
		t.Errorf("got %v", err)
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
	if err := config.ValidateConsumed(); err != nil {
		t.Errorf("got %v", err)
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
	if err := os.WriteFile(path, []byte("version = 9\n[input]\ncontinue = \"go on\"\n"), 0o600); err != nil {
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
