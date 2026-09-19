package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/tool/command"
)

func writeRunnableFile(t *testing.T, path string) {
	t.Helper()

	//nolint:gosec // a tool the test declares has to be runnable
	if err := os.WriteFile(path, []byte("#!/bin/bash\ntrue\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func configDeclaringATool(t *testing.T, body string) (Config, string) {
	t.Helper()

	directory := t.TempDir()
	writeRunnableFile(t, filepath.Join(directory, "forecast"))

	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, undent(body)); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return config, directory
}

func TestADeclaredToolBecomesAToolTheModelIsOffered(t *testing.T) {
	config, _ := configDeclaringATool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["./forecast"]
		parameters = [
			{ name = "city", kind = "string", description = "the city to report on" },
		]
	`)

	tools, err := config.BuildDeclaredTools(command.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(tools) != 1 {
		t.Fatalf("got %d tools", len(tools))
	}
	if tools[0].Name() != "weather" {
		t.Errorf("got name %q", tools[0].Name())
	}
	if tools[0].Description() != "report the weather for a city" {
		t.Errorf("got description %q", tools[0].Description())
	}
}

func TestADeclaredCommandIsResolvedAgainstTheConfigThatSuppliedIt(t *testing.T) {
	config, directory := configDeclaringATool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["./forecast", "--quietly"]
	`)

	declaration, err := config.declare("weather")
	if err != nil {
		t.Fatal(err)
	}

	wanted := []string{filepath.Join(directory, "forecast"), "--quietly"}
	if declaration.Command[0] != wanted[0] || declaration.Command[1] != wanted[1] {
		t.Errorf("got %q, wanted %q", declaration.Command, wanted)
	}
}

func TestADeclaredToolOnThePathIsLeftForThePathToFind(t *testing.T) {
	config, _ := configDeclaringATool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]
	`)

	declaration, err := config.declare("weather")
	if err != nil {
		t.Fatal(err)
	}

	if declaration.Command[0] != "true" {
		t.Errorf("got %q", declaration.Command)
	}
}

func TestADeclaredToolAsksUnlessItSaysOtherwise(t *testing.T) {
	for written, mustAsk := range map[string]bool{"": true, "ask": true, "allow": false} {
		t.Run("permission "+written, func(t *testing.T) {
			body := `
				[tools.weather]
				description = "report the weather for a city"
				command = ["true"]
			`
			if written != "" {
				body += "permission = \"" + written + "\"\n"
			}

			config, _ := configDeclaringATool(t, body)
			declaration, err := config.declare("weather")
			if err != nil {
				t.Fatal(err)
			}
			if declaration.MustAsk != mustAsk {
				t.Errorf("got %v", declaration.MustAsk)
			}
		})
	}
}

func TestADeclaredToolNamesItsConfigWhenItCannotBeBuilt(t *testing.T) {
	config, _ := configDeclaringATool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["/nowhere/at/all"]
	`)

	_, err := config.BuildDeclaredTools(command.Options{})
	if err == nil || !strings.Contains(err.Error(), "tools.weather: could not find /nowhere/at/all") {
		t.Errorf("got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("the failure did not name the config: %v", err)
	}
}

func TestAnUnreadablePermissionIsRefusedByName(t *testing.T) {
	config, _ := configDeclaringATool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]
		permission = "whenever"
	`)

	_, err := config.BuildDeclaredTools(command.Options{})
	if err == nil || !strings.Contains(err.Error(), "tools.weather: permission:") {
		t.Errorf("got %v", err)
	}
}

func TestAWorkspaceMayNotDeclareAToolOfItsOwn(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "[tools.weather]\ndescription = \"x\"\n"); err != nil {
		t.Fatal(err)
	}

	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[tools.weather]\ndescription = \"y\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSources(Source{Path: path}, Source{Path: overridePath, IsOverride: true})
	if err == nil || !strings.Contains(err.Error(), "cannot be overridden in oh.toml") {
		t.Errorf("got %v", err)
	}
}

func TestAWorkspaceMayNotOpenAToolTableAtAll(t *testing.T) {
	directory := t.TempDir()
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[tools.weather]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSources(Source{Path: overridePath, IsOverride: true})
	if err == nil || !strings.Contains(err.Error(), "cannot be overridden in oh.toml") {
		t.Errorf("got %v", err)
	}
}

func TestADeclaredToolReachesTheNextSession(t *testing.T) {
	if got := ReachOf("tools.weather"); got != ReachNextSession {
		t.Errorf("got reach %v", got)
	}
}

func TestAToolIsADefaultUnlessItSaysOtherwise(t *testing.T) {
	config, _ := configDeclaringATool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]

		[tools.hostfact]
		description = "report a fact about the host"
		command = ["true"]
		default = false

		[tools.lookup]
		default = false

		[tools.grep]
		default = true
	`)

	wanted := []string{"hostfact", "lookup"}
	if got := config.NonDefaultToolNames(); strings.Join(got, ",") != strings.Join(wanted, ",") {
		t.Errorf("got %q, wanted %q", got, wanted)
	}
}

func TestAnEntryWithNoCommandNamesAToolTheHarnessAlreadyOffers(t *testing.T) {
	config, _ := configDeclaringATool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]

		[tools.lookup]
		default = false

		[tools.notify]
		default = false
	`)

	wanted := []string{"lookup", "notify"}
	if got := config.ToolReferenceNames(); strings.Join(got, ",") != strings.Join(wanted, ",") {
		t.Errorf("got %q, wanted %q", got, wanted)
	}

	tools, err := config.BuildDeclaredTools(command.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name() != "weather" {
		t.Errorf("a reference was built as a tool of its own: %v", tools)
	}
}

func TestAnEntryNamingAnOfferedToolMaySayNothingButWhetherItIsADefault(t *testing.T) {
	for setting, body := range map[string]string{
		"description": `description = "something else"`,
		"subject":     `subject = "query"`,
		"timeout":     `timeout = "5s"`,
		"permission":  `permission = "allow"`,
		"parameters":  `parameters = [{ name = "x", kind = "string", description = "x" }]`,
	} {
		t.Run(setting, func(t *testing.T) {
			config, _ := configDeclaringATool(t, "[tools.lookup]\ndefault = false\n"+body+"\n")

			_, err := config.BuildDeclaredTools(command.Options{})
			if err == nil || !strings.Contains(err.Error(), setting+" names a tool this harness already offers") {
				t.Errorf("got %v", err)
			}
		})
	}
}

func writeToolbox(t *testing.T, body string) string {
	t.Helper()

	directory := t.TempDir()
	writeRunnableFile(t, filepath.Join(directory, "forecast"))

	path := filepath.Join(directory, "toolbox.toml")
	if err := os.WriteFile(path, []byte(undent(body)), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestAToolboxDeclaresToolsAndResolvesTheirCommandsAgainstItself(t *testing.T) {
	path := writeToolbox(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["./forecast", "--quietly"]

		[tools.lookup]
		default = false
	`)

	contents, err := LoadToolbox(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(contents.Tools) != 2 {
		t.Fatalf("got %d declarations", len(contents.Tools))
	}
	if wanted := filepath.Join(filepath.Dir(path), "forecast"); contents.Tools["weather"].Command[0] != wanted {
		t.Errorf("got %q, wanted %q", contents.Tools["weather"].Command, wanted)
	}
	if contents.Tools["lookup"].IsDefault() {
		t.Error("expected lookup to be no default")
	}
}

func TestAToolboxCarriesNoSettingButItsTools(t *testing.T) {
	path := writeToolbox(t, `
		version = 10

		[ui]
		streaming = "asap"

		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]
	`)

	_, err := LoadToolbox(path)
	if err == nil || !strings.Contains(err.Error(), "unknown: ui, ui.streaming, version") {
		t.Errorf("got %v", err)
	}
}

func TestAToolboxThatIsNotThereIsRefused(t *testing.T) {
	_, err := LoadToolbox(filepath.Join(t.TempDir(), "nothing.toml"))
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("got %v", err)
	}
}

func TestAToolboxReplacesAConfigToolOfTheSameName(t *testing.T) {
	config, _ := configDeclaringATool(t, `
		[tools.weather]
		description = "the config version"
		command = ["true"]
	`)

	path := writeToolbox(t, `
		[tools.weather]
		description = "the toolbox version"
		command = ["true"]
	`)

	contents, err := LoadToolbox(path)
	if err != nil {
		t.Fatal(err)
	}
	config = config.WithTools(contents.Tools)

	if got := config.Tools["weather"].Description; got != "the toolbox version" {
		t.Errorf("got %q", got)
	}
}

func TestAnEnvironmentCarriesItsPromptAsTextOrAsAFile(t *testing.T) {
	inline := writeToolbox(t, `
		prompt = "You are the cook."

		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]
	`)

	contents, err := LoadToolbox(inline)
	if err != nil {
		t.Fatal(err)
	}
	if contents.Prompt.Text != "You are the cook." {
		t.Errorf("got %q", contents.Prompt.Text)
	}

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "cook.md"), []byte("  You are the cook.  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fromFile := filepath.Join(directory, "env.toml")
	if err := os.WriteFile(fromFile, []byte("prompt = { file = \"cook.md\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	contents, err = LoadToolbox(fromFile)
	if err != nil {
		t.Fatal(err)
	}
	if contents.Prompt.Text != "You are the cook." {
		t.Errorf("got %q", contents.Prompt.Text)
	}
}

func TestAToolboxWithNoPromptCarriesNone(t *testing.T) {
	path := writeToolbox(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]
	`)

	contents, err := LoadToolbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if contents.Prompt.Text != "" {
		t.Errorf("got %q", contents.Prompt.Text)
	}
}

func TestAnUnusablePromptIsRefusedForTheReasonItIsUnusable(t *testing.T) {
	for name, test := range map[string]struct{ body, wording string }{
		"empty text":    {`prompt = "  "`, "prompt is empty"},
		"table no file": {`prompt = { other = "x" }`, "prompt is a table, so it wants a file"},
		"not text":      {`prompt = 7`, "prompt is not text or a table naming a file"},
		"missing file":  {`prompt = { file = "nowhere.md" }`, "prompt.file"},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeToolbox(t, test.body+"\n")
			if _, err := LoadToolbox(path); err == nil || !strings.Contains(err.Error(), test.wording) {
				t.Errorf("got %v, wanted %s", err, test.wording)
			}
		})
	}
}

func TestACommandResolvesEveryPathItWritesRelativeToTheToolbox(t *testing.T) {
	path := writeToolbox(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["deno", "run", "--allow-sys", "./forecast", "--store", "./state.json"]
	`)

	contents, err := LoadToolbox(path)
	if err != nil {
		t.Fatal(err)
	}

	beside := filepath.Dir(path)
	wanted := []string{
		"deno", "run", "--allow-sys",
		filepath.Join(beside, "forecast"),
		"--store", filepath.Join(beside, "state.json"),
	}

	if got := contents.Tools["weather"].Command; strings.Join(got, " ") != strings.Join(wanted, " ") {
		t.Errorf("got %q, wanted %q", got, wanted)
	}
}

func TestACommandLeavesAlonePlainWordsAndAbsolutePaths(t *testing.T) {
	path := writeToolbox(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["deno", "run", "--allow-read=/var/lib/weather", "/opt/forecast", "--fact", "uptime"]
	`)

	contents, err := LoadToolbox(path)
	if err != nil {
		t.Fatal(err)
	}

	wanted := "deno run --allow-read=/var/lib/weather /opt/forecast --fact uptime"
	if got := strings.Join(contents.Tools["weather"].Command, " "); got != wanted {
		t.Errorf("got %q, wanted %q", got, wanted)
	}
}
