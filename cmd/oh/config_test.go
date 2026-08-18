package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredSkillDirectoriesResolvesAbsoluteRelativeAndHomePaths(t *testing.T) {
	configurationDirectory := t.TempDir()
	path := filepath.Join(configurationDirectory, "config.toml")
	absolute := filepath.Join(t.TempDir(), "skills")
	home := t.TempDir()
	t.Setenv("HOME", home)
	contents := "model = \"configured-model\"\neffort = \"low\"\n[skill]\nlookup = [\"" + absolute + "\", \"shared/skills\", \"~/.system/config/pi/agent/skills\"]\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := loadConfiguredSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Model != "configured-model" || settings.Effort != "low" {
		t.Errorf("got model %q and effort %q", settings.Model, settings.Effort)
	}
	directories := settings.Skill.LookupDirs
	want := []string{
		absolute,
		filepath.Join(configurationDirectory, "shared", "skills"),
		filepath.Join(home, ".system", "config", "pi", "agent", "skills"),
	}
	if len(directories) != len(want) {
		t.Fatalf("got %#v, want %#v", directories, want)
	}
	for index := range want {
		if directories[index] != want[index] {
			t.Errorf("directory %d is %q, want %q", index, directories[index], want[index])
		}
	}
}

func TestConfiguredSettingsAllowsNoSettingsFile(t *testing.T) {
	settings, err := loadConfiguredSettings(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if settings.Skill.LookupDirs != nil || settings.Sandbox.Read != nil ||
		settings.Sandbox.Write != nil || settings.Sandbox.Exec != nil {
		t.Errorf("got %#v, want no configured paths", settings)
	}
}

func TestConfiguredSkillDirectoriesRejectsAnEmptyDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[skill]\nlookup = [\"\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := loadConfiguredSettings(path); err == nil {
		t.Error("expected an empty skill directory to be rejected")
	}
}

func TestConfiguredModelAndEffortCannotBeEmpty(t *testing.T) {
	for name, contents := range map[string]string{
		"model":  "model = \"\"\n",
		"effort": "effort = \"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadConfiguredSettings(path); err == nil {
				t.Errorf("expected empty %s to be rejected", name)
			}
		})
	}
}

func TestConfiguredAccessPathsAreResolved(t *testing.T) {
	configurationDirectory := t.TempDir()
	path := filepath.Join(configurationDirectory, "config.toml")
	home := t.TempDir()
	t.Setenv("HOME", home)
	contents := "[sandbox]\nread = [\"~/reference\"]\nwrite = [\"output\"]\nexec = [\"/opt/tools\"]\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := loadConfiguredSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	assertPaths := func(name string, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s got %#v, want %#v", name, got, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Errorf("%s path %d is %q, want %q", name, index, got[index], want[index])
			}
		}
	}
	assertPaths("read", settings.Sandbox.Read, []string{filepath.Join(home, "reference")})
	assertPaths("write", settings.Sandbox.Write, []string{filepath.Join(configurationDirectory, "output")})
	assertPaths("exec", settings.Sandbox.Exec, []string{"/opt/tools"})
}
