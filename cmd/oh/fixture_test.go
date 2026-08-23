package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestFixtureOutputsAreCompleteAndOwned(t *testing.T) {
	expected := map[string]string{}
	claimFixtureOutputs(t, expected, "replay input", "testdata/input", ".jsonl", []string{
		".ansi",
		".screen",
		".transcript",
	})
	claimFixtureOutputs(t, expected, "generated scenario", "testdata/scenarios", ".toml", []string{
		".ansi",
		".jsonl",
		".requests.jsonl",
		".screen",
		".transcript",
	})
	for name, extensions := range map[string][]string{
		"banner":     {".ansi", ".screen"},
		"inputblock": {".ansi", ".screen"},
		"lifecycle":  {".ansi", ".screen"},
		"running":    {".ansi", ".screen"},
	} {
		claimFixtureName(t, expected, "special replay", name, extensions)
	}

	outputDirectory := filepath.Join("testdata", "output")
	entries, err := os.ReadDir(outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("unexpected output directory %s", entry.Name())
			continue
		}
		actual[entry.Name()] = struct{}{}
		if _, exists := expected[entry.Name()]; !exists {
			if *updateGoldens {
				if err := os.Remove(filepath.Join(outputDirectory, entry.Name())); err != nil {
					t.Error(err)
				}
				continue
			}
			t.Errorf("orphaned output %s", entry.Name())
		}
	}

	if *updateGoldens {
		return
	}
	for name, owner := range expected {
		if _, exists := actual[name]; !exists {
			t.Errorf("%s is missing output %s", owner, name)
		}
	}
}

func claimFixtureOutputs(
	t *testing.T,
	expected map[string]string,
	owner string,
	directory string,
	sourceExtension string,
	outputExtensions []string,
) {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != sourceExtension {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), sourceExtension)
		claimFixtureName(t, expected, owner+" "+entry.Name(), name, outputExtensions)
	}
}

func claimFixtureName(
	t *testing.T,
	expected map[string]string,
	owner string,
	name string,
	extensions []string,
) {
	t.Helper()

	for _, extension := range extensions {
		output := name + extension
		if previousOwner, duplicate := expected[output]; duplicate {
			t.Errorf("%s and %s both own %s", previousOwner, owner, output)
			continue
		}
		expected[output] = owner
	}
}

func TestFixtureSourceNamesAreUnique(t *testing.T) {
	owners := map[string]string{}
	for directory, extension := range map[string]string{
		"testdata/input":     ".jsonl",
		"testdata/scenarios": ".toml",
	} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), extension)
			if previousOwner, duplicate := owners[name]; duplicate {
				t.Errorf("duplicate fixture name %q in %s and %s", name, previousOwner, directory)
			}
			owners[name] = directory
		}
	}

	if len(owners) == 0 {
		t.Fatal("no fixture sources")
	}
}

func TestTestdataContainsNoPersonalOrSecretMaterial(t *testing.T) {
	patterns := map[string]*regexp.Regexp{
		"absolute home path": regexp.MustCompile(`/(?:home|Users)/[^\s"\\]+`),
		"credential":         regexp.MustCompile(`(?i)(?:authorization|bearer[[:space:]]+[A-Za-z0-9._~+/-]{12,}|access_token|refresh_token|api[_-]?key|sk-[A-Za-z0-9]{12,}|eyJ[A-Za-z0-9_-]{12,}\.)`),
		"email address":      regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
		"host address":       regexp.MustCompile(`(?:^|[^0-9.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?`),
		"UUID":               regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`),
	}

	err := filepath.WalkDir("testdata", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path) //nolint:gosec // fixed testdata tree
		if err != nil {
			return err
		}
		for name, pattern := range patterns {
			if pattern.Match(contents) {
				t.Errorf("%s contains %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
