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
	"crdx.org/io/cmd/oh/work"
)

const (
	defaultGlobalContext = "You are a helpful coding assistant."
	globalContextName    = "SYSTEM.md"
)

var (
	projectContextNames    = []string{"AGENTS.md", "AGENTS.local.md"}
	harnessContextTemplate = template.Must(template.New("harness").Funcs(template.FuncMap{
		"filesystem":    filesystem,
		"filepathJoin":  filepath.Join,
		"scopeRules":    scopeRules,
		"shellAccess":   shellAccess,
		"webAccess":     webAccess,
		"networkRules":  networkRules,
		"scratchRules":  scratchRules,
		"shellSandbox":  shellSandbox,
		"sandboxHeader": sandboxHeader,
	}).Parse(hereduck.D(`
		{{ sandboxHeader .Yolo }}# Scope

		- Your workspace is the current directory, {{ .WorkspaceDir }}
		- Your session is named {{ .SessionName }}
		{{ scopeRules .ExtraPaths .CurrentCaps }}

		# Personality

		- Use casual lowercase when chatting with the user, but write normally everywhere else
		- Adopt the personality of the animal in your session name, and use its emoji

		# Network

		{{ networkRules . }}

		# /tmp

		{{ scratchRules .TmpDir .Yolo }}
		- If you encounter a read-only workspace, follow this process:
			- Copy the current workspace into your scratch space
			- Do the work there, then produce a *.patch file the user can apply to their repo
			- Tell the user to apply it with: cd <workspace> && git apply <user's path to patch>

		# Home

		- HOME is {{ .HomeDir }}, which is exclusively for you and your agent companions
		- A tilde (~) for you is not the same as for the user. The user has their own HOME.
		- Every path on the user's machine, including the ones above, is written here in full
		- Write them the same way back, and never abbreviate one to a tilde

		# State

		- The workspace ({{ .WorkspaceDir }}) is {{ filesystem .WorkspaceWritable }}
		- The .git directory within it ({{ filepathJoin .WorkspaceDir ".git" }}) is {{ filesystem .GitWritable }}
		- The bash tool is {{ shellAccess .ShellGranted }}{{ shellSandbox .Yolo }}

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
	ShellGranted      bool
	WebGranted        bool
	Yolo              bool
}

func ProjectContextPaths(workspace *work.Space) []string {
	paths := make([]string, 0, len(projectContextNames))
	for _, name := range projectContextNames {
		paths = append(paths, filepath.Join(workspace.GetDir(), name))
	}
	return paths
}

type File struct {
	Name string
	Body string
}

type Config struct {
	GlobalPath  string
	Workspace   *work.Space
	SessionName string
	TmpDir      string
	HomeDir     string
	CurrentCaps caps.Set
	ExtraPaths  shell.Paths
	Skills      []skill.Skill
	Yolo        bool
}

func Load(config Config) (string, []File, error) {
	globalFile, err := readGlobalContext(config.GlobalPath)
	if err != nil {
		return "", nil, err
	}

	projectFiles, err := readProjectContext(config.Workspace.GetRoot())
	if err != nil {
		return "", nil, err
	}

	files := projectFiles
	if globalFile != nil {
		files = append([]File{*globalFile}, projectFiles...)
	}

	return mergeContexts(
		harnessContext(config),
		globalContext(globalFile),
		projectContext(projectFiles),
		skill.Context(config.Skills),
	), files, nil
}

func readContextFile(name string, read func() ([]byte, error)) (*File, error) {
	data, err := read()
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil //nolint:nilnil // an absent context file is nothing to report
	case err != nil:
		return nil, err
	}

	if strings.TrimSpace(string(data)) == "" {
		return nil, nil //nolint:nilnil // an empty context file is nothing to report
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

func harnessContext(config Config) string {
	currentCaps := config.CurrentCaps
	data := harnessContextTemplateData{
		WorkspaceDir:      config.Workspace.GetDir(),
		SessionName:       config.SessionName,
		TmpDir:            config.TmpDir,
		HomeDir:           config.HomeDir,
		CurrentCaps:       currentCaps,
		ExtraPaths:        config.ExtraPaths,
		WorkspaceWritable: currentCaps.Has(caps.Write),
		GitWritable:       currentCaps.Has(caps.Git),
		ShellGranted:      currentCaps.Has(caps.Shell),
		WebGranted:        currentCaps.Has(caps.Web),
		Yolo:              config.Yolo,
	}

	var renderedText strings.Builder
	if err := harnessContextTemplate.Execute(&renderedText, data); err != nil {
		panic(err)
	}
	return strings.TrimSpace(renderedText.String())
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

func filesystem(isWritable bool) string {
	if isWritable {
		return "read-write"
	}

	return "read-only"
}

func shellAccess(isGranted bool) string {
	if isGranted {
		return "granted"
	}

	return "refused"
}

func webAccess(isGranted bool) string {
	if isGranted {
		return "granted external network access"
	}

	return "refused"
}

func sandboxHeader(isYolo bool) string {
	if !isYolo {
		return ""
	}

	return hereduck.D(`
		# No Sandbox

		- This session was started with --yolo, so the bash tool runs with no sandbox at all
		- A command can read, write, delete, and reach the network as freely as the user can
		- Nothing stops a mistake, so read a destructive command back to yourself before running it
		- The states below still govern the file tools; hold the bash tool to them yourself
	`) + "\n"
}

func networkRules(data harnessContextTemplateData) string {
	if data.Yolo {
		return strings.Join([]string{
			"- There is no sandbox, so a command reaches whatever network this machine reaches",
			"- The web search and fetch tools are " + webAccess(data.WebGranted),
		}, "\n")
	}

	return strings.Join([]string{
		"- Networking is limited to the sandbox's private loopback interface",
		"- Processes in the same sandbox can communicate over 127.0.0.1 and ::1",
		"- A Unix socket works beneath /tmp, and is refused beneath the workspace",
		"- The host's loopback interface and external networks are unreachable",
		"- The web search and fetch tools are " + webAccess(data.WebGranted),
		"- Anything else that requires external networking must be asked of the user",
	}, "\n")
}

func scratchRules(tmpDir string, isYolo bool) string {
	if isYolo {
		return strings.Join([]string{
			"- /tmp is the machine's own /tmp, shared with everything else running on it",
			"- Your persistent scratch space is " + tmpDir + ", which you can always read and write to",
			"- Give the user that path exactly as it is written here",
		}, "\n")
	}

	return strings.Join([]string{
		"- /tmp is your persistent scratch space, which you can always read and write to",
		"- It maps to " + tmpDir + " on the user's machine, so bear that in mind",
		"- Always translate /tmp paths to the user's equivalent path before giving it to them",
		"\t- For example: /tmp/foo.png → " + filepath.Join(tmpDir, "foo.png"),
	}, "\n")
}

func shellSandbox(isYolo bool) string {
	if isYolo {
		return ", and runs unconfined"
	}

	return ""
}
