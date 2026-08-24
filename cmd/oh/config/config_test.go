package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/scrollOverflow"
	"crdx.org/io/cmd/oh/segment/workingDirectory"
)

func TestConfiguredSkillDirectoriesResolvesAbsoluteRelativeAndHomePaths(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.toml")
	absolute := filepath.Join(t.TempDir(), "skills")
	home := t.TempDir()
	t.Setenv("HOME", home)
	contents := "editor = \"  subl  \"\nget_on_with_it_message = \"carry on\"\n[model]\nround_robin = [\"opencode/deepseek@hi\"]\n[skill]\ninclude = [\"" + absolute + "\", \"shared/skills\", \"~/.system/config/pi/agent/skills\"]\n"
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
	if config.Editor != "subl" {
		t.Errorf("got editor %q", config.Editor)
	}
	if config.GetOnWithItMessage != "carry on" {
		t.Errorf("got get-on-with-it message %q", config.GetOnWithItMessage)
	}
	directories := config.Skill.Include
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

func TestAMissingConfigFileIsAllowed(t *testing.T) {
	config, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Skill.Include) != 0 || len(config.Sandbox.Read) != 0 ||
		len(config.Sandbox.Write) != 0 || len(config.Sandbox.Exec) != 0 {
		t.Errorf("got %#v, want no configured paths", config)
	}
	if config.Editor != "" {
		t.Errorf("got default editor %q", config.Editor)
	}
	if config.GetOnWithItMessage != "yes" {
		t.Errorf("got default get-on-with-it message %q", config.GetOnWithItMessage)
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
	if err := writeConfigFile(path, "[skill]\ninclude = [\"\"]\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Error("expected an empty skill directory to be rejected")
	}
}

func TestConfiguredSkillExclusionsResolveToAbsoluteDirectories(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.toml")
	if err := writeConfigFile(path, "[skill]\nexclude = [\"skills/pi\"]\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configDir, "skills", "pi")
	if len(config.Skill.Exclude) != 1 || config.Skill.Exclude[0] != want {
		t.Errorf("got exclusions %#v, want [%s]", config.Skill.Exclude, want)
	}
}

func TestConfiguredSkillExclusionsRejectAnEmptyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "[skill]\nexclude = [\"\"]\n"); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Error("expected an empty skill directory to be rejected")
	}
}

func TestConfiguredStringsCannotBeEmpty(t *testing.T) {
	for name, contents := range map[string]string{
		"model round robin":                 "[model]\nround_robin = []\n",
		"model selection":                   "[model]\nround_robin = [\"\"]\n",
		"model selection whitespace":        "[model]\nround_robin = [\"  \"]\n",
		"get_on_with_it_message":            "get_on_with_it_message = \"\"\n",
		"get_on_with_it_message whitespace": "get_on_with_it_message = \"  \"\n",
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

func TestTheConfiguredGetOnWithItMessageIsTrimmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeConfigFile(path, "get_on_with_it_message = \"  carry on  \"\n"); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.GetOnWithItMessage != "carry on" {
		t.Errorf("got get-on-with-it message %q", config.GetOnWithItMessage)
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
		left = [{ segment = "working-directory" }, { segment = "mode-toggle" }]
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
		center = [{ segment = "working-directory", loudly = true }]
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
		"version", "model", "get_on_with_it_message", "skill", "sandbox", "bar",
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
		"activity-spinner":  inertFactory,
		"context-usage":     inertFactory,
		"mode-toggle":       inertFactory,
		"working-directory": workingDirectory.New("/tmp/somewhere"),
		"active-model":      inertFactory,
		"scroll-overflow":   scrollOverflow.New,
		"current-session":   inertFactory,
		"current-time":      inertFactory,
		"turn-elapsed":      inertFactory,
		"turn-count":        inertFactory,
		"last-tps":          inertFactory,
		"git-branch":        inertFactory,
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
