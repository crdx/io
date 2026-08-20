// Package skill discovers agent skills and makes them available.
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/util"
	"gopkg.in/yaml.v3"
)

const (
	filename      = "SKILL.md"
	directoryName = "skills"
)

// Skill is the part of a skill disclosed before its body is read.
type Skill struct {
	Name        string
	Description string
	Location    string

	directory string
	isGlobal  bool
}

type metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Counts reports how many discovered skills came from each scope.
func Counts(skills []Skill) (int, int) {
	var project int
	var global int

	for _, foundSkill := range skills {
		if foundSkill.isGlobal {
			global++
		} else {
			project++
		}
	}

	return project, global
}

// ExcludeGlobal removes globally-scoped skills in configured absolute directories.
func ExcludeGlobal(skills []Skill, directories []string) []Skill {
	excludedDirectories := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		excludedDirectories[directory] = struct{}{}
	}

	filteredSkills := make([]Skill, 0, len(skills))
	for _, foundSkill := range skills {
		_, excluded := excludedDirectories[foundSkill.directory]
		if foundSkill.isGlobal && excluded {
			continue
		}
		filteredSkills = append(filteredSkills, foundSkill)
	}

	return filteredSkills
}

// NameFromPath reports the skill a path is the SKILL.md of.
func NameFromPath(path string) (string, bool) {
	if filepath.Base(path) != filename {
		return "", false
	}

	directory := filepath.Dir(path)
	if filepath.Base(filepath.Dir(directory)) != directoryName {
		return "", false
	}

	return filepath.Base(directory), true
}

// Discover finds skills in the project scope and each global skills directory.
func Discover(project string, globalDirectories []string, warnings io.Writer) ([]Skill, error) {
	projectSkills, err := discover(filepath.Join(project, ".agents", directoryName), false, warnings)
	if err != nil {
		return nil, err
	}

	discoveredSkills := projectSkills
	seenDirectories := make(map[string]struct{}, len(globalDirectories))
	for _, directory := range globalDirectories {
		if directory == "" {
			continue
		}

		directory = filepath.Clean(directory)
		if _, seen := seenDirectories[directory]; seen {
			continue
		}
		seenDirectories[directory] = struct{}{}

		globalSkills, err := discover(directory, true, warnings)
		if err != nil {
			return nil, err
		}
		discoveredSkills = append(discoveredSkills, globalSkills...)
	}

	return discoveredSkills, nil
}

func discover(scope string, isGlobal bool, warnings io.Writer) ([]Skill, error) {
	scopeRoot, err := os.OpenRoot(scope)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not scan skills in %s: %w", pathutil.Shorten(scope), err)
	}
	defer func() { _ = scopeRoot.Close() }()

	directory, err := scopeRoot.Open(".")
	if err != nil {
		return nil, fmt.Errorf("could not scan skills in %s: %w", pathutil.Shorten(scope), err)
	}
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		return nil, fmt.Errorf("could not scan skills in %s: %w", pathutil.Shorten(scope), err)
	}
	slices.SortFunc(entries, func(left, right fs.DirEntry) int {
		return strings.Compare(left.Name(), right.Name())
	})

	var out []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		directory := filepath.Join(scope, entry.Name())
		location := filepath.Join(directory, filename)
		shownLocation := pathutil.Shorten(location) // warned about, rather than read from
		data, err := scopeRoot.ReadFile(filepath.Join(entry.Name(), filename))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			util.WriteWarningf(warnings, "%s: %v", shownLocation, err)
			continue
		}

		skillMetadata, err := parse(data)
		if err != nil {
			util.WriteWarningf(warnings, "%s: %v", shownLocation, err)
			continue
		}
		skillMetadata.Name = strings.TrimSpace(skillMetadata.Name)
		skillMetadata.Description = strings.TrimSpace(skillMetadata.Description)
		if skillMetadata.Description == "" {
			util.WriteWarningf(warnings, "%s: skill description is missing", shownLocation)
			continue
		}
		if skillMetadata.Name == "" {
			skillMetadata.Name = entry.Name()
		}
		if skillMetadata.Name != entry.Name() {
			util.WriteWarningf(warnings, "%s: skill name %q does not match directory %q", shownLocation, skillMetadata.Name, entry.Name())
		}

		absoluteLocation, err := filepath.Abs(location)
		if err != nil {
			util.WriteWarningf(warnings, "%s: could not resolve location: %v", shownLocation, err)
			continue
		}

		out = append(out, Skill{
			Name:        skillMetadata.Name,
			Description: skillMetadata.Description,
			Location:    absoluteLocation,
			directory:   filepath.Dir(absoluteLocation),
			isGlobal:    isGlobal,
		})
	}

	return out, nil
}

func parse(data []byte) (metadata, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) == 0 || string(bytes.TrimSpace(lines[0])) != "---" {
		return metadata{}, errors.New("skill has no YAML frontmatter")
	}

	end := -1
	for index := 1; index < len(lines); index++ {
		if string(bytes.TrimSpace(lines[index])) == "---" {
			end = index
			break
		}
	}
	if end == -1 {
		return metadata{}, errors.New("skill has unterminated YAML frontmatter")
	}

	var parsedMetadata metadata
	if err := yaml.Unmarshal(bytes.Join(lines[1:end], []byte("\n")), &parsedMetadata); err != nil {
		return metadata{}, fmt.Errorf("could not parse skill frontmatter: %w", err)
	}

	return parsedMetadata, nil
}

// MountGlobalSkills makes globally-scoped skills readable by file tools.
func MountGlobalSkills(root *file.Root, skills []Skill) ([]*os.Root, error) {
	var openedRoots []*os.Root

	for _, foundSkill := range skills {
		if !foundSkill.isGlobal {
			continue
		}

		mountedRoot, err := os.OpenRoot(foundSkill.directory)
		if err != nil {
			closeRoots(openedRoots)
			return nil, fmt.Errorf("could not mount skill %s: %w", foundSkill.Name, err)
		}

		root.Mount(foundSkill.directory, file.New(mountedRoot, func(string) error { return file.ErrReadOnly }))
		openedRoots = append(openedRoots, mountedRoot)
	}

	return openedRoots, nil
}

// Close closes roots returned by MountGlobalSkills.
func Close(roots []*os.Root) {
	closeRoots(roots)
}

func closeRoots(roots []*os.Root) {
	for _, root := range roots {
		_ = root.Close()
	}
}
