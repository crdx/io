package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"crdx.org/hereduck"
	"crdx.org/io/cmd/oh/caps"
	"crdx.org/io/cmd/oh/conditions"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/cmd/oh/skill"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/internal/util/strutil"
)

const (
	shellToolName  = "bash"
	jobToolName    = "job"
	lookupToolName = "lookup"
	fetchToolName  = "fetch"

	clipboardDropsHeading = "# Clipboard Drops"
	defaultGlobalContext  = "You are a helpful coding assistant."
	globalContextName     = "SYSTEM.md"
)

var (
	projectContextNames    = []string{"AGENTS.md", "AGENTS.local.md"}
	harnessContextTemplate = template.Must(template.New("harness").Funcs(template.FuncMap{
		"filesystem":     filesystem,
		"filepathJoin":   filepath.Join,
		"scopeRules":     scopeRules,
		"shellAccess":    shellAccess,
		"lookupAccess":   lookupAccess,
		"networkSection": networkSection,
		"stateRules":     stateRules,
		"scratchRules":   scratchRules,
		"homeWriteRule":  homeWriteRule,
		"shellSandbox":   shellSandbox,
		"sandboxHeader":  sandboxHeader,
	}).Parse(hereduck.D(`
		{{ sandboxHeader .Yolo }}# Harness

		- "oh" is the harness you are running within
		- Each session dir is under {{ .SessionsDir }}, named after the session
		- This session's directory is {{ .SessionDir }}
		- "session.jsonl" is the journal, the single source of truth, as JSONL
		- "meta.json" is the listing entry: name, title, timestamps, and message count
		- "chat.md" is the readable transcript of the conversation
		- "wire.http" is the raw traffic between the harness and the model endpoint
		- The user's settings are in {{ .ConfigFile }}, and their instructions in {{ .GlobalPath }}
		- A session name said with no other context is a hint to read that session's files

		# Scope

		- Your workspace is the current directory, {{ .WorkspaceDir }}
		- Your session is named {{ .SessionName }}
		{{ scopeRules . }}

		# Personality

		- Use casual lowercase when chatting with the user, but write normally everywhere else
		- Adopt the personality of the animal in your session name, and use its emoji

		{{ networkSection . }}# /tmp

		{{ scratchRules . }}

		# Home

		- HOME is {{ .HomeDir }}, which is exclusively for you and your agent companions
		{{ homeWriteRule . }}
		- A tilde (~) for you is not the same as for the user. The user has their own HOME.
		- Every path on the user's machine, including the ones above, is written here in full
		- Write them the same way back, and never abbreviate one to a tilde

		# State

		{{ stateRules . }}

		These states can change at any time. You will be told what changed when it does.
		When one of these states blocks the work, ask the user to change that state.
	`)))
)

type harnessContextTemplateData struct {
	WorkspaceDir      string
	SessionName       string
	SessionsDir       string
	SessionDir        string
	ConfigFile        string
	GlobalPath        string
	TmpDir            string
	HomeDir           string
	ExtraPaths        shell.Paths
	DropsDirectory    string
	ShellOffered      bool
	LookupOffered     bool
	FetchOffered      bool
	Conditions        conditions.Conditions
	WorkspaceWritable bool
	IsRepository      bool
	GitWritable       bool
	ShellGranted      bool
	LookupGranted     bool
	JobsGranted       bool
	NetworkGranted    bool
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
	GlobalPath     string
	Workspace      *work.Space
	SessionName    string
	SessionsDir    string
	SessionDir     string
	ConfigFile     string
	TmpDir         string
	HomeDir        string
	CurrentCaps    caps.Set
	ExtraPaths     shell.Paths
	DropsDirectory string
	OfferedTools   []string
	Skills         []skill.Skill
	Conditions     conditions.Conditions
	JobsGranted    bool
	NetworkGranted bool
	Yolo           bool
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
		SessionsDir:       config.SessionsDir,
		SessionDir:        config.SessionDir,
		ConfigFile:        config.ConfigFile,
		GlobalPath:        config.GlobalPath,
		TmpDir:            config.TmpDir,
		HomeDir:           config.HomeDir,
		ExtraPaths:        config.ExtraPaths,
		DropsDirectory:    config.DropsDirectory,
		ShellOffered:      isToolOffered(config.OfferedTools, shellToolName),
		LookupOffered:     isToolOffered(config.OfferedTools, lookupToolName),
		FetchOffered:      isToolOffered(config.OfferedTools, fetchToolName),
		Conditions:        config.Conditions,
		WorkspaceWritable: currentCaps.Has(caps.Write),
		IsRepository:      pathutil.Exists(filepath.Join(config.Workspace.GetDir(), ".git")),
		GitWritable:       currentCaps.Has(caps.Git),
		ShellGranted:      currentCaps.Has(caps.Shell),
		JobsGranted:       config.JobsGranted && isToolOffered(config.OfferedTools, jobToolName),
		NetworkGranted:    config.NetworkGranted,
		LookupGranted:     currentCaps.Has(caps.Lookup),
		Yolo:              config.Yolo,
	}

	var renderedText strings.Builder
	if err := harnessContextTemplate.Execute(&renderedText, data); err != nil {
		panic(err)
	}
	return strings.TrimSpace(renderedText.String())
}

func WithDropsDirectory(systemPrompt string, dropsDirectory string) string {
	if dropsDirectory == "" {
		return systemPrompt
	}
	rule := dropsRule(dropsDirectory)
	if strings.Contains(systemPrompt, rule) {
		return systemPrompt
	}
	return strings.TrimSpace(systemPrompt) + "\n\n" + clipboardDropsHeading + "\n\n- " + rule
}

func dropsRule(dropsDirectory string) string {
	return "Pasting a clipboard image with ctrl+v saves it under " + dropsDirectory + ", where path tools can read it."
}

func scopeRules(data harnessContextTemplateData) string {
	extraPaths := data.ExtraPaths
	dropsDirectory := data.DropsDirectory

	var lines []string

	switch {
	case dropsDirectory != "":
		lines = append(lines, "- Tools that accept a path can access the workspace, private home, /tmp, and the paths listed here.")
	case len(extraPaths.Read)+len(extraPaths.Write) > 0:
		lines = append(lines, "- Tools that accept a path can access the workspace, private home, /tmp, and the configured paths listed here.")
	default:
		lines = append(lines, "- Tools that accept a path can only access the workspace, private home, and /tmp.")
	}

	if dropsDirectory != "" {
		lines = append(lines, "- "+dropsRule(dropsDirectory))
	}
	for _, path := range extraPaths.Read {
		lines = append(lines, "- The configured path "+path+" is read-only"+scratchException(path, data)+".")
	}
	for _, path := range extraPaths.Write {
		lines = append(lines, "- The configured path "+path+" is "+
			filesystem(data.WorkspaceWritable)+" and follows the workspace write state.")
	}
	if data.ShellOffered && !data.Yolo {
		lines = append(lines, "- The shell may also read the system directories, "+
			"but it can only write where the path tools can.")
		lines = append(lines, "- The shell may execute files under the system directories, "+
			"every directory in PATH, the workspace, HOME, and /tmp.")
		for _, path := range extraPaths.Exec {
			lines = append(lines, "- The shell may also execute files at or under "+path+".")
		}
	}

	return strings.Join(lines, "\n")
}

func scratchException(path string, data harnessContextTemplateData) string {
	if data.TmpDir == "" {
		return ""
	}
	if _, isCovered := pathutil.RelativeTo(path, data.TmpDir); !isCovered {
		return ""
	}

	return ", apart from your scratch space at " + data.TmpDir + ", which is writable"
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

func lookupAccess(isGranted bool) string {
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

		- This session was started with --yolo, so the bash tool runs with no sandbox
		- All commands can read, write, delete, and reach the network as freely as the user can
		- Nothing stops a mistake, so read a destructive command back to yourself before running it
		- The states below still govern the file tools; hold the bash tool to them yourself
	`) + "\n"
}

func isToolOffered(offeredTools []string, name string) bool {
	return len(offeredTools) == 0 || slices.Contains(offeredTools, name)
}

func networkSection(data harnessContextTemplateData) string {
	rules := networkRules(data)
	if rules == "" {
		return ""
	}

	return "# Network\n\n" + rules + "\n\n"
}

func stateRules(data harnessContextTemplateData) string {
	lines := []string{
		"- The workspace (" + data.WorkspaceDir + ") is " + filesystem(data.WorkspaceWritable),
	}

	if data.IsRepository {
		lines = append(lines, "- The .git directory within it ("+
			filepath.Join(data.WorkspaceDir, ".git")+") is "+filesystem(data.GitWritable))
	} else {
		lines = append(lines, "- The workspace is not a git repository")
	}

	if data.ShellOffered {
		lines = append(lines, "- The bash tool is "+shellAccess(data.ShellGranted)+shellSandbox(data.Yolo))
		if !data.Yolo {
			lines = append(lines, "- A process a bash call leaves running is killed when that call ends"+
				jobSurvival(data.JobsGranted))
		}
	}

	return strings.Join(lines, "\n")
}

func jobSurvival(areJobsGranted bool) string {
	if !areJobsGranted {
		return ""
	}

	return ", so start anything that must outlive the call with the job tool"
}

func networkRules(data harnessContextTemplateData) string {
	if !data.ShellOffered {
		return strings.Join(networkToolRules(data), "\n")
	}

	if data.Yolo {
		return strings.Join(append(
			[]string{"- There is no network sandbox: everything runs on the host network"},
			networkToolRules(data)...,
		), "\n")
	}

	loopback := "- Processes in the same sandbox can communicate over 127.0.0.1"
	if data.Conditions.IPv6 {
		loopback += " and ::1"
	} else {
		loopback += ", and this machine has no IPv6 at all"
	}

	lines := []string{
		"- A bash call has no network other than the sandbox's private loopback interface",
		loopback,
	}

	if data.JobsGranted {
		lines = append(
			lines,
			"- A service started with the job tool stays running, and can be reached on 127.0.0.1 afterwards",
		)
	}

	canRequestHostNetwork := data.NetworkGranted
	hostReachability := unreachableRule(
		"the host's loopback interface and external networks are unreachable",
		canRequestHostNetwork,
	)
	if len(data.ExtraPaths.HostLoopback) > 0 {
		ports := make([]string, len(data.ExtraPaths.HostLoopback))
		for i, port := range data.ExtraPaths.HostLoopback {
			ports[i] = strconv.Itoa(int(port))
		}
		subject := "TCP ports " + strings.Join(ports, ", ")
		verb := " are"
		destination := "ports"
		if len(ports) == 1 {
			subject = "TCP port " + ports[0]
			verb = " is"
			destination = "port"
		}
		lines = append(
			lines,
			"- The host's loopback "+subject+verb+" reachable on the same sandbox loopback "+destination,
		)
		hostReachability = unreachableRule(
			"all other host loopback traffic and external networks are unreachable",
			canRequestHostNetwork,
		)
	}

	lines = append(lines, unixSocketRule(data.Conditions.UnixSockets), hostReachability)
	lines = append(lines, networkToolRules(data)...)

	return strings.Join(append(lines, hostNetworkRules(data.NetworkGranted, data.Conditions.Interactive)...), "\n")
}

func homeWriteRule(data harnessContextTemplateData) string {
	if data.Yolo {
		return "- You can write anywhere inside HOME"
	}

	return "- HOME is writable only while the workspace is writable, " +
		"though .cache inside HOME is always writable"
}

func unixSocketRule(areUnixSocketsReachable bool) string {
	if areUnixSocketsReachable {
		return "- A Unix socket works beneath /tmp, and is refused beneath the workspace"
	}

	return "- A Unix socket is unavailable whatever its path, so nothing can listen on one"
}

func unreachableRule(text string, canRequest bool) string {
	if canRequest {
		return "- By default, " + text
	}

	return "- " + strutil.Capitalise(text)
}

func networkToolRules(data harnessContextTemplateData) []string {
	var lines []string

	if data.LookupOffered {
		lines = append(lines, "- The lookup tool is "+lookupAccess(data.LookupGranted))
	}
	if data.FetchOffered {
		lines = append(lines, "- The fetch tool is "+lookupAccess(data.NetworkGranted))
	}

	return lines
}

func hostNetworkRules(isNetworkGranted bool, isInteractive bool) []string {
	lines := []string{
		"- The bash tool takes network=loopback or network=host, and defaults to loopback",
	}

	if !isNetworkGranted {
		lines = append(lines,
			"- The host network is withheld in this session, so a call asking for network=host is "+
				"refused before the command runs")
		if isInteractive {
			return append(
				lines,
				"- The user can grant the host network with ctrl+x n",
				"- Ask the user to grant the host network rather than asking the user to run the command",
			)
		}

		return append(lines,
			"- The user cannot grant the host network here, so keep to the sandbox's private loopback")
	}

	lines = append(
		lines,
		"- A call with network=host runs on the host's own network instead of the private loopback",
		"- A host call reaches the internet, the local network, and the host's own loopback listeners",
		"- A host call cannot reach the sandbox's private loopback, so a sandbox service is out of reach",
	)

	if isInteractive {
		lines = append(
			lines,
			"- The user may be asked to approve each host call, and may refuse the call or let it time out",
		)
	} else {
		lines = append(
			lines,
			"- The user cannot approve a host call, so expect refusal unless permission is granted in advance",
		)
	}

	return append(
		lines,
		"- Ask for the host network only when the work needs the host network, and say why in the call",
	)
}

func scratchRules(data harnessContextTemplateData) string {
	var lines []string

	if data.Yolo {
		lines = []string{
			"- /tmp is the machine's own /tmp, shared with everything else running on it",
			"- Your persistent scratch space is " + data.TmpDir + ", which you can always read and write to",
			"- Give the user that path exactly as it is written here",
		}
	} else {
		lines = []string{
			"- /tmp is your persistent scratch space, which you can always read and write to",
			"- It maps to " + data.TmpDir + " on the user's machine, so bear that in mind",
			"- Always translate /tmp paths to the user's equivalent path before giving it to them",
			"\t- For example: /tmp/foo.png → " + filepath.Join(data.TmpDir, "foo.png"),
		}
	}

	if data.ShellOffered {
		lines = append(lines, readOnlyWorkspaceRules...)
	}

	return strings.Join(lines, "\n")
}

var readOnlyWorkspaceRules = []string{
	"- If you encounter a read-only workspace, follow this process:",
	"\t- Clone it into your scratch space with: git clone --shared <workspace> <destination>",
	"\t- Bring uncommitted work across with: git -C <workspace> diff HEAD | git -C <destination> apply",
	"\t- Copy untracked files you need by hand, and use cp -r only when the workspace is not a repository",
	"\t- Do the work there, then produce a *.patch file the user can apply to their repo",
	"\t- Tell the user to apply it with: cd <workspace> && git apply <user's path to patch>",
}

func shellSandbox(isYolo bool) string {
	if isYolo {
		return ", and runs unconfined"
	}

	return ""
}
