package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"crdx.org/hereduck"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
)

const (
	defaultGlobalContext = "You are a helpful coding assistant."
	globalContextName    = "SYSTEM.md"
)

var (
	projectContextNames    = []string{"AGENTS.md", "AGENTS.local.md"}
	harnessContextTemplate = template.Must(template.New("harness").Funcs(template.FuncMap{
		"background":   background,
		"filesystem":   filesystem,
		"filepathJoin": filepath.Join,
		"scopeRules":   scopeRules,
		"shellAccess":  shellAccess,
		"webAccess":    webAccess,
	}).Parse(hereduck.D(`
		# Scope

		- Your workspace is the current directory, {{ .WorkspaceDir }}
		- Your session is named {{ .SessionName }}
		{{ scopeRules .ExtraPaths .CurrentCaps }}

		# Network

		- Networking is limited to the sandbox's private loopback interface
		- Processes in the same sandbox can communicate over 127.0.0.1 and ::1
		- The host's loopback interface and external networks are unreachable
		- The web search and fetch tools are {{ webAccess .WebGranted }}
		- Anything else that requires external networking must be asked of the user

		# /tmp

		- /tmp is your persistent private scratch space
		- It's always read-write, and no other agents have access to yours
		- /tmp maps to {{ .TmpDir }} on the user's machine
		- Translate /tmp paths to that directory before giving them to the user
			- For example: /tmp/result.png → {{ filepathJoin .TmpDir "result.png" }}
		- /tmp is persistent so you should tidy up after yourself and clean up large artifacts
			- For example: git clones, source trees, binaries

		# Home

		- HOME is {{ .HomeDir }}, which is scratch space for you rather than the user's home
		- A ~ in the shell means that directory, so a ~ path never reaches anything of the user's
		- Every path on the user's machine, including the ones above, is written here in full
		- Write them the same way back, and never abbreviate one to a ~

		# State

		- The workspace ({{ .WorkspaceDir }}) is {{ filesystem .WorkspaceWritable }}
		- The .git directory within it ({{ filepathJoin .WorkspaceDir ".git" }}) is {{ filesystem .GitWritable }}
		- Background processes are {{ background .BackgroundEnabled }}
		- The bash tool is {{ shellAccess .ShellGranted }}

		These states can change at any time. You will be told what changed when it does.
	`)))
)

type harnessContextTemplateData struct {
	WorkspaceDir      string
	SessionName       string
	TmpDir            string
	HomeDir           string
	CurrentCaps       caps.Set
	ExtraPaths        shell.Paths
	WorkspaceWritable bool
	GitWritable       bool
	BackgroundEnabled bool
	ShellGranted      bool
	WebGranted        bool
}

func ProjectContextPaths(workspaceDir string) []string {
	paths := make([]string, 0, len(projectContextNames))
	for _, name := range projectContextNames {
		paths = append(paths, filepath.Join(workspaceDir, name))
	}
	return paths
}

type File struct {
	Name string
	Body string
}

type Config struct {
	GlobalPath   string
	Root         *os.Root
	WorkspaceDir string
	SessionName  string
	TmpDir       string
	HomeDir      string
	CurrentCaps  caps.Set
	ExtraPaths   shell.Paths
	Skills       []skill.Skill
}

func Load(config Config) (string, []File, error) {
	globalFile, err := readGlobalContext(config.GlobalPath)
	if err != nil {
		return "", nil, err
	}

	projectFiles, err := readProjectContext(config.Root)
	if err != nil {
		return "", nil, err
	}

	files := projectFiles
	if globalFile != nil {
		files = append([]File{*globalFile}, projectFiles...)
	}

	return mergeContexts(
		harnessContext(
			config.WorkspaceDir,
			config.SessionName,
			config.TmpDir,
			config.HomeDir,
			config.CurrentCaps,
			config.ExtraPaths,
		),
		globalContext(globalFile),
		projectContext(projectFiles),
		skill.Context(config.Skills),
	), files, nil
}

func readContextFile(name string, read func() ([]byte, error)) (*File, error) {
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

	return &File{Name: name, Body: string(data)}, nil
}

func readGlobalContext(path string) (*File, error) {
	file, err := readContextFile(globalContextName, func() ([]byte, error) {
		return os.ReadFile(path) //nolint:gosec // this is the one documented config path
	})
	if err != nil {
		return nil, fmt.Errorf("could not read the system context %s: %w", path, err)
	}

	return file, nil
}

func readProjectContext(root *os.Root) ([]File, error) {
	var files []File

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

func globalContext(file *File) string {
	if file == nil {
		return defaultGlobalContext
	}

	return file.Body
}

func harnessContext(workspaceDir string, sessionName string, tmpDir string, homeDir string, currentCaps caps.Set, extraPaths shell.Paths) string {
	data := harnessContextTemplateData{
		WorkspaceDir:      workspaceDir,
		SessionName:       sessionName,
		TmpDir:            tmpDir,
		HomeDir:           homeDir,
		CurrentCaps:       currentCaps,
		ExtraPaths:        extraPaths,
		WorkspaceWritable: currentCaps.Has(caps.Write),
		GitWritable:       currentCaps.Has(caps.Git),
		BackgroundEnabled: currentCaps.Has(caps.Background),
		ShellGranted:      currentCaps.Has(caps.Shell),
		WebGranted:        currentCaps.Has(caps.Web),
	}

	var rendered strings.Builder
	if err := harnessContextTemplate.Execute(&rendered, data); err != nil {
		panic(err)
	}
	return strings.TrimSpace(rendered.String())
}

func scopeRules(extraPaths shell.Paths, currentCaps caps.Set) string {
	var lines []string

	if len(extraPaths.Read)+len(extraPaths.Write) > 0 {
		lines = append(lines, "- Tools that accept a path can access the workspace, private home, /tmp, and the configured paths listed here.")
	} else {
		lines = append(lines, "- Tools that accept a path can only access the workspace, private home, and /tmp.")
	}

	for _, path := range extraPaths.Read {
		lines = append(lines, "- The configured path "+path+" is read-only.")
	}
	for _, path := range extraPaths.Write {
		lines = append(lines, "- The configured path "+path+" is "+
			filesystem(currentCaps.Has(caps.Write))+" and follows the workspace write state.")
	}
	for _, path := range extraPaths.Exec {
		lines = append(lines, "- The shell may execute files at or under "+path+".")
	}

	return strings.Join(lines, "\n")
}

func projectContext(files []File) string {
	if len(files) == 0 {
		return ""
	}

	sections := make([]string, 0, len(files))
	for _, file := range files {
		sections = append(sections, "## "+file.Name+"\n\n"+strings.TrimSpace(file.Body))
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

func webAccess(granted bool) string {
	if granted {
		return "granted external network access"
	}

	return "refused"
}

func background(enabled bool) string {
	if enabled {
		return "allowed to outlive shell commands"
	}

	return "killed when their shell command ends"
}
