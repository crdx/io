package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"crdx.org/hereduck"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/internal/pathutil"
)

const (
	defaultGlobalContext = "You are a helpful coding assistant."
	globalContextName    = "SYSTEM.md"
)

var projectContextNames = []string{"AGENTS.md", "AGENTS.local.md"}

type contextFile struct {
	name string // what the file is called
	body string // its contents
}

func loadContext(
	root *os.Root,
	workspaceDir string,
	tmpDir string,
	currentCaps caps,
	extraPaths configuredPaths,
	skills []skill.Skill,
) (string, []contextFile, error) {
	globalFile, err := readGlobalContext()
	if err != nil {
		return "", nil, err
	}

	projectFiles, err := readProjectContext(root)
	if err != nil {
		return "", nil, err
	}

	files := projectFiles
	if globalFile != nil {
		files = append([]contextFile{*globalFile}, projectFiles...)
	}

	return mergeContexts(
		harnessContext(workspaceDir, tmpDir, currentCaps, extraPaths),
		globalContext(globalFile),
		projectContext(projectFiles),
		skill.Context(skills),
	), files, nil
}

func readContextFile(name string, read func() ([]byte, error)) (*contextFile, error) {
	data, err := read()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}

	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}

	return &contextFile{name: name, body: string(data)}, nil
}

func readGlobalContext() (*contextFile, error) {
	path := globalContextPath()

	file, err := readContextFile(globalContextName, func() ([]byte, error) {
		return os.ReadFile(path) //nolint:gosec // this is the one documented config path
	})
	if err != nil {
		return nil, fmt.Errorf("could not read the system context %s: %w", path, err)
	}

	return file, nil
}

func readProjectContext(root *os.Root) ([]contextFile, error) {
	var files []contextFile

	for _, name := range projectContextNames {
		file, err := readContextFile(name, func() ([]byte, error) { return root.ReadFile(name) })
		if err != nil {
			return nil, fmt.Errorf("could not read the project context %s: %w", name, err)
		}

		if file != nil {
			files = append(files, *file)
		}
	}

	return files, nil
}

func globalContext(file *contextFile) string {
	if file == nil {
		return defaultGlobalContext
	}

	return file.body
}

func harnessContext(workspaceDir string, tmpDir string, currentCaps caps, extraPaths configuredPaths) string {
	return mergeContexts(
		scopeSection(workspaceDir, extraPaths, currentCaps),
		networkSection(),
		tmpSection(tmpDir),
		stateSection(workspaceDir, currentCaps),
	)
}

func scopeSection(workspaceDir string, extraPaths configuredPaths, currentCaps caps) string {
	return hereduck.Df(
		`
		# Scope

		- Your workspace is the current directory, %s.
		%s
	`,
		workspaceDir,
		scopeRules(extraPaths, currentCaps),
	)
}

func networkSection() string {
	return hereduck.D(`
		# Network

		- Networking is limited to the sandbox's private loopback interface.
		- Processes in the same sandbox can communicate over 127.0.0.1 and ::1.
		- The host's loopback interface and external networks are unreachable.
		- Anything that requires external networking must be asked of the user.
	`)
}

func tmpSection(tmpDir string) string {
	tmpDir = pathutil.Shorten(tmpDir)

	return hereduck.Df(
		`
		# /tmp

		- /tmp is your persistent private scratch space.
		- It is always read-write.
		- No other agents have access to yours.
		- /tmp maps to %s on the user's machine.
		- Translate /tmp paths to that directory before giving them to the user.
		- For example: /tmp/result.png → %s
	`,
		tmpDir,
		filepath.Join(tmpDir, "result.png"),
	)
}

func stateSection(workspaceDir string, currentCaps caps) string {
	return hereduck.Df(
		`
		# State

		%s

		These states can change at any time. You will be told what changed when it does.

		%s
	`,
		stateRules(workspaceDir, currentCaps),
		researchNote(currentCaps),
	)
}

func researchNote(currentCaps caps) string {
	if currentCaps.has(capWrite) {
		return ""
	}

	return "If the workspace is read-only you should consider any task you're given to be a research task."
}

func stateRules(workspaceDir string, currentCaps caps) string {
	return strings.Join([]string{
		"- The workspace (" + workspaceDir + ") is " + filesystem(currentCaps.has(capWrite)),
		"- The .git directory within it (" + filepath.Join(workspaceDir, ".git") + ") is " +
			filesystem(currentCaps.has(capGit)),
		"- Background processes are " + background(currentCaps.has(capBackground)),
		"- The bash tool is " + shellAccess(currentCaps.has(capShell)),
	}, "\n")
}

func scopeRules(extraPaths configuredPaths, currentCaps caps) string {
	var lines []string

	if len(extraPaths.Read)+len(extraPaths.Write) > 0 {
		lines = append(lines, "- Tools that accept a path can access the workspace, /tmp, and the configured paths listed here.")
	} else {
		lines = append(lines, "- Tools that accept a path can only access the workspace and /tmp.")
	}

	for _, path := range extraPaths.Read {
		lines = append(lines, "- The configured path "+path+" is read-only.")
	}
	for _, path := range extraPaths.Write {
		lines = append(lines, "- The configured path "+path+" is "+
			filesystem(currentCaps.has(capWrite))+" and follows the workspace write state.")
	}
	for _, path := range extraPaths.Exec {
		lines = append(lines, "- The shell may execute files under "+path+".")
	}

	return strings.Join(lines, "\n")
}

func projectContext(files []contextFile) string {
	if len(files) == 0 {
		return ""
	}

	sections := make([]string, 0, len(files))
	for _, file := range files {
		sections = append(sections, "## "+file.name+"\n\n"+strings.TrimSpace(file.body))
	}

	return "# Project Context\n\n" + strings.Join(sections, "\n\n")
}

func mergeContexts(sections ...string) string {
	out := sections[:0]
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			out = append(out, section)
		}
	}
	return strings.Join(out, "\n\n")
}

func filesystem(writable bool) string {
	if writable {
		return "read-write"
	}

	return "read-only"
}

func shellAccess(granted bool) string {
	if granted {
		return "granted"
	}

	return "refused"
}

func background(enabled bool) string {
	if enabled {
		return "allowed to outlive shell commands"
	}

	return "killed when their shell command ends"
}
