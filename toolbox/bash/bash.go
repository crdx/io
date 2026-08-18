// Package bash runs shell commands under caller-supplied sandbox policies.
package bash

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/pathutil"
	"crdx.org/io/internal/sandbox"
	"crdx.org/io/internal/strutil"
	"crdx.org/io/tool"
)

// Args is what a shell command takes.
type Args struct {
	Command string `json:"command"` // the command to run
}

// New builds a shell that starts in root and is confined anew for each command, what is granted
// being the caller's to change in between. An error from fresh turns the command away, so a caller
// that withholds the shell says so there. readOnly answers for display, without building a policy
// to ask one.
func New(
	root *file.Root,
	readOnly func() bool,
	fresh func(context.Context) (sandbox.Policy, error),
	processes *sandbox.Processes,
) tool.Tool {
	return shell{
		Tool: tool.Syntax(tool.DefineMeasured(
			"bash",
			"run a shell command",
			tool.Schema{
				tool.String("command", "the command line"),
			},
			Render,
			func(ctx context.Context, args Args) (string, tool.Statistics, error) {
				policy, err := fresh(ctx)
				if err != nil {
					return "", tool.Statistics{}, err
				}
				return exec(ctx, root, policy, args, processes)
			},
		), "bash"),

		readOnly: readOnly,
	}
}

type shell struct {
	tool.Tool

	readOnly func() bool
}

func (self shell) ReadOnly() bool { return self.readOnly() }

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

// Render flattens a command to one display-safe line and reports its original line count.
func Render(args Args) (string, string) {
	return oneLine(args.Command), spread(args.Command)
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
		return fmt.Sprintf("%d lines", lines)
	}

	return ""
}

func exec(
	ctx context.Context,
	root *file.Root,
	policy sandbox.Policy,
	args Args,
	processes *sandbox.Processes,
) (string, tool.Statistics, error) {
	if strings.TrimSpace(args.Command) == "" {
		return "", tool.Statistics{}, errors.New("command is required")
	}

	result, err := processes.Run(ctx, root.Name(), args.Command, policy)
	stats := tool.Statistics{
		Kind: tool.StatsResources, CPUTime: result.CPUTime, PeakMemory: result.PeakMemory,
	}
	if err != nil {
		return "", stats, err
	}

	reportText := report(result, policy)
	stats.Lines = int64(len(strutil.Lines(reportText)))
	stats.Bytes = int64(len(reportText))
	stats.TotalBytes = stats.Bytes

	if result.Code != 0 {
		return reportText, stats, ErrCommandFailed
	}

	return reportText, stats, nil
}

// ErrCommandFailed marks a nonzero command exit; output is still returned.
var ErrCommandFailed = errors.New("the command failed")

func report(result sandbox.Result, policy sandbox.Policy) string {
	output := result.Output

	if result.Code == 0 {
		return output
	}

	status := fmt.Sprintf("exited %d", result.Code)
	if output != "" {
		status += ":"
	}

	parts := []string{status}
	if output != "" {
		parts = append(parts, output)
	}

	switch {
	case matches(output, denials):
		parts = append(parts, note(policy))
	case matches(output, overruns):
		parts = append(parts, "note: the sandbox stopped this command for using too much.")
	}

	return strings.Join(parts, "\n")
}

var denials = []string{
	"Permission denied",
	"Operation not permitted",
	"Address family not supported",
}

var overruns = []string{ // the wording a limit reaches the shell as
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
