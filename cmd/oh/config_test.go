package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/segment"
)

func TestConfiguredSkillDirectoriesResolvesAbsoluteRelativeAndHomePaths(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.toml")
	absolute := filepath.Join(t.TempDir(), "skills")
	home := t.TempDir()
	t.Setenv("HOME", home)
	contents := "provider = \"opencode-go\"\nmodel = \"configured-model\"\neffort = \"low\"\nget_on_with_it_message = \"carry on\"\n[skill]\ninclude = [\"" + absolute + "\", \"shared/skills\", \"~/.system/config/pi/agent/skills\"]\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider != "opencode-go" || config.Model != "configured-model" || config.Effort != "low" {
		t.Errorf("got provider %q, model %q, and effort %q", config.Provider, config.Model, config.Effort)
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
	config, err := loadConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Skill.Include) != 0 || len(config.Sandbox.Read) != 0 ||
		len(config.Sandbox.Write) != 0 || len(config.Sandbox.Exec) != 0 {
		t.Errorf("got %#v, want no configured paths", config)
	}
	if config.GetOnWithItMessage != "yes" {
		t.Errorf("got default get-on-with-it message %q", config.GetOnWithItMessage)
	}
}

func TestConfiguredSkillDirectoriesRejectsAnEmptyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[skill]\ninclude = [\"\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadConfig(path); err == nil {
		t.Error("expected an empty skill directory to be rejected")
	}
}

func TestConfiguredSkillExclusionsResolveToAbsoluteDirectories(t *testing.T) {
	configDir := t.TempDir()
	path := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(path, []byte("[skill]\nexclude = [\"skills/pi\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(path)
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
	if err := os.WriteFile(path, []byte("[skill]\nexclude = [\"\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadConfig(path); err == nil {
		t.Error("expected an empty skill directory to be rejected")
	}
}

func TestConfiguredStringsCannotBeEmpty(t *testing.T) {
	for name, contents := range map[string]string{
		"model":                             "model = \"\"\n",
		"effort":                            "effort = \"\"\n",
		"get_on_with_it_message":            "get_on_with_it_message = \"\"\n",
		"get_on_with_it_message whitespace": "get_on_with_it_message = \"  \"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadConfig(path); err == nil {
				t.Errorf("expected empty %s to be rejected", name)
			}
		})
	}
}

func TestTheConfiguredGetOnWithItMessageIsTrimmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("get_on_with_it_message = \"  carry on  \"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(path)
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
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(path)
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
	if err := os.WriteFile(path, []byte("[sandbox]\nhome = [\"/etc/gitconfig\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "sandbox.home") {
		t.Errorf("the error does not name the setting: %v", err)
	}
}

func TestAConfigSayingNothingAboutTheBarTakesTheBuiltInLayout(t *testing.T) {
	layout := layoutFrom(t, "")

	if got := len(layout[segment.BottomLeft]); got != 5 {
		t.Errorf("expected five segments along the bottom, got %d", got)
	}
	if got := len(layout[segment.TopRight]); got != 1 {
		t.Errorf("expected one segment at the top right, got %d", got)
	}
}

func TestWhatAConfigDoesNotMentionKeepsItsDefault(t *testing.T) {
	layout := layoutFrom(t, `
		[bar.bottom]
		left = [{ segment = "workdir" }, { segment = "modes" }]
	`)

	if got := len(layout[segment.BottomLeft]); got != 2 {
		t.Errorf("expected what the file said, got %d segments", got)
	}
	if got := len(layout[segment.TopRight]); got != 1 {
		t.Errorf("expected the default kept at the top right, got %d segments", got)
	}
	if got := len(layout[segment.BottomRight]); got != 1 {
		t.Errorf("expected the default kept at the bottom right, got %d segments", got)
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
		center = [{ segment = "scroll", direction = "sideways" }]
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
		center = [{ segment = "workdir", loudly = true }]
	`)

	if _, err := config.layout(testSegments()); err != nil {
		t.Fatal(err)
	}

	err := config.unknownKeys()
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
		"provider", "model", "effort", "get_on_with_it_message", "skill", "sandbox", "bar",
	} {
		if _, ok := written[key]; !ok {
			t.Errorf("expected the defaults to say what %q is", key)
		}
	}
}

func testSegments() segment.Registry {
	held := &Harness{mode: caps.NewMode(caps.Read)}

	return availableSegments("/tmp/somewhere", "gpt-5.6-sol", "high", held)
}

func configFrom(t *testing.T, body string) Config {
	t.Helper()

	if body == "" {
		config, err := loadConfig("")
		if err != nil {
			t.Fatal(err)
		}

		return config
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(undent(body)), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := loadConfig(path)
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

	layout, err := configFrom(t, body).layout(testSegments())
	if err != nil {
		t.Fatal(err)
	}

	return layout
}

func brokenLayout(t *testing.T, body string) (segment.Layout, error) {
	t.Helper()

	return configFrom(t, body).layout(testSegments())
}

func builtInConfig(t *testing.T) Config {
	t.Helper()

	config, err := loadConfig("")
	if err != nil {
		t.Fatal(err)
	}

	return config
}
