package bash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/syntax"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/tool"
)

// Args is what a shell command takes.
type Args struct {
	Command string `json:"command"`
}

// New builds a shell that starts in root and is confined anew for each command.
func New(
	root *file.Root,
	fresh func(context.Context) (sandbox.Policy, error),
) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "bash",
			Description: "run a shell command",
			Schema: tool.Schema{
				tool.String("command", "the command line"),
			},
		},
		Describe,
	).
		Validate(validate).
		SyntaxFrom("bash", emphasisSource).
		Stats(func(ctx context.Context, args Args) (string, tool.Stats, error) {
			policy, err := fresh(ctx)
			if err != nil {
				return "", tool.Stats{}, err
			}
			return exec(ctx, root, policy, args)
		})
}

// ProtectedPolicy makes .git at each writable root read-only. It does not search nested
// directories.
func ProtectedPolicy(policy sandbox.Policy) sandbox.Policy {
	var readOnlyPaths []string
	for _, path := range policy.Write {
		gitDir := filepath.Join(path, ".git")

		if pathutil.Exists(gitDir) && !slices.Contains(policy.Read, gitDir) && !slices.Contains(readOnlyPaths, gitDir) {
			readOnlyPaths = append(readOnlyPaths, gitDir)
		}
	}

	return policy.WithRead(readOnlyPaths...)
}

// Describe formats a command into one display-safe line and reports its original line count.
func Describe(args Args) (string, string) {
	parsed, err := parse(args.Command)
	if err != nil {
		return oneLine(args.Command), spread(args.Command)
	}

	if hasHereDocument(parsed) {
		return strutil.FirstLine(args.Command), spread(args.Command)
	}

	return format(parsed), spread(args.Command)
}

func emphasisSource(args Args, subject string) string {
	source := strings.TrimSpace(args.Command)
	if strings.ContainsAny(source, "\r\n") && strings.HasPrefix(source, subject) {
		return source
	}

	return ""
}

func hasHereDocument(parsed *syntax.File) bool {
	found := false

	syntax.Walk(parsed, func(node syntax.Node) bool {
		if redirect, ok := node.(*syntax.Redirect); ok && redirect.Hdoc != nil {
			found = true
		}

		return !found
	})

	return found
}

func validate(args Args) error {
	if strings.TrimSpace(args.Command) == "" {
		return errors.New("command is required")
	}

	if _, err := parse(args.Command); err != nil {
		return fmt.Errorf("invalid Bash command: %w", err)
	}

	return nil
}

func parse(command string) (*syntax.File, error) {
	return syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
}

func format(parsed *syntax.File) string {
	var output bytes.Buffer
	if err := syntax.NewPrinter(syntax.SingleLine(true)).Print(&output, parsed); err != nil {
		panic(err)
	}

	command := strings.TrimSuffix(output.String(), "\n")
	if strings.ContainsAny(command, "\n\r") {
		return oneLine(command)
	}

	return command
}

func oneLine(command string) string {
	var out strings.Builder
	separated := true

	for line := range strings.FieldsFuncSeq(command, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}

		if out.Len() > 0 {
			if separated {
				out.WriteByte(' ')
			} else {
				out.WriteString("; ")
			}
		}

		out.WriteString(line)
		separated = hasSeparator(line)
	}

	return out.String()
}

func hasSeparator(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true
	}

	if strings.ContainsRune(";&|<>", rune(line[len(line)-1])) || strings.HasSuffix(line, `\`) {
		return true
	}

	fields := strings.Fields(line)
	last := fields[len(fields)-1]

	return last == "then" || last == "do" || last == "else" || last == "{"
}

func spread(command string) string {
	if lines := strings.Count(strings.TrimRight(command, "\n"), "\n") + 1; lines > 1 {
		return fmt.Sprintf("%dL", lines)
	}

	return ""
}

func exec(
	ctx context.Context,
	root *file.Root,
	policy sandbox.Policy,
	args Args,
) (string, tool.Stats, error) {
	result, err := sandbox.Run(ctx, root.Name(), args.Command, policy)
	stats := tool.Stats{
		Kind:       tool.StatsResources,
		CPUTime:    result.CPUTime,
		PeakMemory: result.PeakMemory,
	}
	if err != nil {
		return measured(unfinished(result.Output, err), &stats), stats, err
	}

	reportText := measured(report(result, policy), &stats)

	if result.Code != 0 {
		return reportText, stats, ErrCommandFailed
	}

	return reportText, stats, nil
}

// ErrCommandFailed marks a nonzero command exit; output is still returned.
var ErrCommandFailed = errors.New("the command failed")

func measured(reportText string, stats *tool.Stats) string {
	stats.Lines = int64(len(strutil.Lines(reportText)))
	stats.Bytes = int64(len(reportText))
	stats.TotalBytes = stats.Bytes

	return reportText
}

func unfinished(output string, err error) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}

	return strings.TrimRight(output, "\n") + "\nnote: " + err.Error() + "."
}

func report(result sandbox.Result, policy sandbox.Policy) string {
	output := result.Output

	if result.Code == 0 {
		return output
	}

	status := fmt.Sprintf("exit(%d)", result.Code)
	if output != "" {
		status += ":"
	}

	parts := []string{status}
	if output != "" {
		parts = append(parts, output)
	}

	switch killedNote := killed(result, policy); {
	case killedNote != "":
		parts = append(parts, killedNote)
	case matches(output, denials):
		parts = append(parts, note(policy))
	case matches(output, overruns):
		parts = append(parts, "note: the sandbox stopped this command for using too much.")
	}

	return strings.Join(parts, "\n")
}

const signalled = 128

func endingSignal(result sandbox.Result) (syscall.Signal, bool) {
	if result.Signal != 0 {
		return result.Signal, true
	}

	if result.Code > signalled {
		return syscall.Signal(result.Code - signalled), false
	}

	return 0, false
}

func killed(result sandbox.Result, policy sandbox.Policy) string {
	signal, isObserved := endingSignal(result)

	name := unix.SignalName(signal)
	if name == "" {
		return ""
	}

	opening := fmt.Sprintf("note: the shell reports that a process was killed by %s.", name)
	if isObserved {
		opening = fmt.Sprintf("note: the command was killed by %s.", name)
	}

	lines := []string{opening}

	switch signal {
	case syscall.SIGKILL, syscall.SIGXCPU:
		lines = append(lines, processorLimit(result, policy)...)
	case syscall.SIGXFSZ:
		lines = append(lines, fmt.Sprintf(
			"the sandbox lets a command write no more than %s to a single file.",
			util.FormatBytes(policy.FileSize, 3),
		))
	}

	return strings.Join(lines, "\n")
}

func processorLimit(result sandbox.Result, policy sandbox.Policy) []string {
	if policy.CPUTime <= 0 {
		return nil
	}

	return []string{
		fmt.Sprintf(
			"the sandbox gives each process %s of processor time, counted across every thread it runs,"+
				" and stops the whole command after %s of wall clock.",
			util.CompactDuration(policy.CPUTime), util.CompactDuration(policy.Timeout),
		),
		fmt.Sprintf(
			"every process this command started used %s of processor time between them.",
			util.CompactDuration(result.CPUTime),
		),
	}
}

var denials = []string{
	"Permission denied",
	"Operation not permitted",
	"Address family not supported",
}

var overruns = []string{
	"File size limit exceeded",
	"Cpu time limit exceeded",
	"Too many open files",
}

func matches(output string, wordings []string) bool {
	for _, wording := range wordings {
		if strings.Contains(output, wording) {
			return true
		}
	}

	return false
}

func note(policy sandbox.Policy) string {
	lines := []string{
		"note: this command ran in a sandbox, and the failure may be the sandbox refusing it.",
		"external networks and the host's loopback interface are unavailable.",
		"writable: " + strings.Join(policy.Write, ", "),
	}

	if len(policy.Read) > 0 {
		lines = append(lines, "readable: "+strings.Join(policy.Read, ", "))
	}

	return strings.Join(lines, "\n")
}
