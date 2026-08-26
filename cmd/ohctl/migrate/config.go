package migrate

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"crdx.org/io/cmd/oh/config"
	"crdx.org/io/internal/format"
)

type ConfigOptions struct {
	Path   string
	DryRun bool
}

func MigrateConfig(options ConfigOptions) (int, bool, error) {
	data, err := os.ReadFile(options.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return config.Format, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	fromFormat, _, err := readConfigDocument(data)
	if err != nil {
		return 0, true, err
	}
	if err := format.Check(fromFormat, config.Format); err != nil {
		return fromFormat, true, fmt.Errorf("%w: upgrade oh", err)
	}
	if fromFormat == config.Format {
		return fromFormat, true, nil
	}

	migrated := data
	for format := fromFormat; format < config.Format; format++ {
		migrationStep, found := configSteps[format]
		if !found {
			return fromFormat, true, fmt.Errorf("nothing knows how to migrate config format %d", format)
		}
		migrated, err = migrationStep(migrated)
		if err != nil {
			return fromFormat, true, fmt.Errorf("format %d: %w", format, err)
		}
	}
	if options.DryRun {
		return fromFormat, true, nil
	}

	backupPath := configBackupPath(options.Path)
	if err := keepConfigCopy(backupPath, data); err != nil {
		return fromFormat, true, err
	}
	if err := writeConfig(options.Path, migrated); err != nil {
		return fromFormat, true, err
	}

	return fromFormat, true, nil
}

type configStep func(data []byte) ([]byte, error)

var configSteps = map[int]configStep{
	config.InitialFormat:       migrateConfigFromVersionOne,
	config.RoundRobinFormat:    migrateConfigFromVersionTwo,
	config.SegmentNamesFormat:  migrateConfigFromVersionThree,
	config.EditorCommandFormat: migrateConfigFromVersionFour,
}

func migrateConfigFromVersionFour(data []byte) ([]byte, error) {
	return rewriteConfigVersion(data, config.SnippetDefinitionFormat), nil
}

func migrateConfigFromVersionThree(data []byte) ([]byte, error) {
	_, document, err := readConfigDocument(data)
	if err != nil {
		return nil, err
	}

	migrated := data
	if _, hasEditor := document["editor"]; hasEditor {
		migrated = rewriteEditorCommand(migrated)
	}

	return rewriteConfigVersion(migrated, config.EditorCommandFormat), nil
}

func rewriteEditorCommand(data []byte) []byte {
	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	tableStart := len(lines)
	editorAt := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			tableStart = i
			break
		}

		if editorAt < 0 {
			if key, hasKey := configLineKey(line); hasKey && key == "editor" {
				editorAt = i
			}
		}
	}

	if editorAt < 0 {
		return data
	}

	commandLine := editorCommandLine(lines[editorAt])

	migrated := append([]string(nil), lines[:editorAt]...)
	migrated = append(migrated, lines[editorAt+1:tableStart]...)
	migrated = appendWithoutTrailingBlank(migrated, "", "[editor]", commandLine)
	if tableStart < len(lines) {
		migrated = append(migrated, "")
		migrated = append(migrated, lines[tableStart:]...)
	}

	joined := strings.Join(migrated, "\n")
	if hasFinalNewline {
		joined += "\n"
	}

	return []byte(joined)
}

func editorCommandLine(line string) string {
	at := strings.Index(line, "=")
	key := line[:at]
	gap := key[len(strings.TrimRight(key, " \t")):]

	return "command" + gap + line[at:]
}

func migrateConfigFromVersionTwo(data []byte) ([]byte, error) {
	if _, _, err := readConfigDocument(data); err != nil {
		return nil, err
	}

	renamed := renameConfigSegment(data, "current-session", "session-name")
	renamed = renameConfigSegment(renamed, "current-time", "local-time")

	return rewriteConfigVersion(renamed, config.SegmentNamesFormat), nil
}

func renameConfigSegment(data []byte, oldName string, newName string) []byte {
	pattern := regexp.MustCompile(`\bsegment\s*=\s*["']` + regexp.QuoteMeta(oldName) + `["']`)

	return pattern.ReplaceAllFunc(data, func(match []byte) []byte {
		return bytes.Replace(match, []byte(oldName), []byte(newName), 1)
	})
}

func readConfigDocument(data []byte) (int, map[string]any, error) {
	var header struct {
		Version int `toml:"version"`
	}
	if _, err := toml.Decode(string(data), &header); err != nil {
		return 0, nil, err
	}

	var document map[string]any
	if _, err := toml.Decode(string(data), &document); err != nil {
		return 0, nil, err
	}

	if header.Version == 0 {
		header.Version = config.InitialFormat
	}

	return header.Version, document, nil
}

func migrateConfigFromVersionOne(data []byte) ([]byte, error) {
	_, document, err := readConfigDocument(data)
	if err != nil {
		return nil, err
	}

	providerName, hasProvider, err := configString(document, "provider")
	if err != nil {
		return nil, err
	}
	model, hasLegacyModel, err := configString(document, "model")
	if err != nil {
		if _, hasModelTable := document["model"].(map[string]any); !hasModelTable {
			return nil, err
		}
		hasLegacyModel = false
	}
	effort, hasEffort, err := configString(document, "effort")
	if err != nil {
		return nil, err
	}

	selection := ""
	if hasLegacyModel && model != "" {
		if !hasProvider {
			providerName = "codex"
		}
		if !hasEffort {
			effort = defaultConfigEffort(providerName)
		}
		if providerName == "" || effort == "" {
			return nil, errors.New("the legacy model needs both a provider and an effort")
		}
		selection = providerName + "/" + model + "@" + effort
	}

	removed := map[string]bool{
		"version":  true,
		"provider": hasProvider,
		"effort":   hasEffort,
		"model":    hasLegacyModel,
	}

	return rewriteVersionOneConfig(data, removed, selection), nil
}

func configString(document map[string]any, name string) (string, bool, error) {
	value, found := document[name]
	if !found {
		return "", false, nil
	}

	written, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("%s is not a string", name)
	}

	return written, true, nil
}

func defaultConfigEffort(providerName string) string {
	switch providerName {
	case "codex", "anthropic":
		return "high"
	default:
		return ""
	}
}

func rewriteVersionOneConfig(data []byte, removed map[string]bool, selection string) []byte {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	tableStart := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			tableStart = i
			break
		}
	}

	root := make([]string, 0, tableStart+1)
	for _, line := range lines[:tableStart] {
		key, hasKey := configLineKey(line)
		if hasKey && removed[key] {
			continue
		}
		root = append(root, line)
	}

	versionAt := 0
	for versionAt < len(root) {
		trimmed := strings.TrimSpace(root[versionAt])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		versionAt++
	}
	prefix := appendWithoutTrailingBlank(append([]string(nil), root[:versionAt]...))
	if len(prefix) > 0 {
		prefix = append(prefix, "")
	}
	versioned := append(prefix, fmt.Sprintf("version = %d", config.RoundRobinFormat))
	root = append(versioned, root[versionAt:]...)

	if selection != "" {
		root = appendWithoutTrailingBlank(root, "", "[model]", "round_robin = ["+strconv.Quote(selection)+"]", "")
	}

	migrated := append(root, lines[tableStart:]...)

	return []byte(strings.Join(migrated, "\n") + "\n")
}

func rewriteConfigVersion(data []byte, version int) []byte {
	text := string(data)
	hasFinalNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}

		key, hasKey := configLineKey(line)
		if !hasKey || key != "version" {
			continue
		}

		indentation := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lines[i] = fmt.Sprintf("%sversion = %d", indentation, version)
		if _, comment, hasComment := strings.Cut(line, "#"); hasComment {
			lines[i] += " #" + comment
		}
		break
	}

	migrated := strings.Join(lines, "\n")
	if hasFinalNewline {
		migrated += "\n"
	}

	return []byte(migrated)
}

func configLineKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}

	key, _, found := strings.Cut(trimmed, "=")
	if !found {
		return "", false
	}

	return strings.TrimSpace(key), true
}

func appendWithoutTrailingBlank(lines []string, added ...string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return append(lines, added...)
}

func configBackupPath(path string) string {
	return fmt.Sprintf("%s.pre-v%d", path, config.Format)
}

func keepConfigCopy(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the configured path is ours
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("a copy is already kept in %s: move it aside first", path)
	}
	if err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

func writeConfig(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "config-*.toml")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return err
	}

	return os.Rename(temporaryPath, path)
}
