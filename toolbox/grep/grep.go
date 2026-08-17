package grep

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"crdx.org/io/internal/file"
	"crdx.org/io/internal/util"
	"crdx.org/io/tool"
)

// Args is what a search by content takes. An absent path is the working directory, and an absent
// glob every file below it.
type Args struct {
	Pattern string `json:"pattern"` // the regular expression
	Path    string `json:"path"`    // where to search
	Glob    string `json:"glob"`    // which paths to include
}

// New builds the grep tool confined to root. A match is reported as path:line:text.
func New(root *file.Root) tool.Tool {
	definedTool := tool.Define(
		"grep",
		"search file contents",
		tool.Schema{
			tool.String("pattern", "regexp"),
			tool.String("path", "directory, defaults to working directory").Optional(),
			tool.String("glob", "path filter, e.g. **/*.go").Optional(),
		},
		Render,
		func(ctx context.Context, args Args) (string, error) { return run(ctx, root, args) },
	)

	return tool.ReadOnly(tool.Concurrent(tool.Focus(definedTool, util.SearchPath)))
}

// Render describes the search out loud.
func Render(args Args) (string, string) {
	return util.RenderSearch(args.Pattern, args.Path, args.Glob)
}

func run(ctx context.Context, root *file.Root, args Args) (string, error) {
	if args.Pattern == "" {
		return "", errors.New("pattern is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", err
	}

	commandContext, stop := context.WithCancel(ctx)
	defer stop()

	arguments := []string{
		"--no-config",
		"--with-filename",
		"--line-number",
		"--no-heading",
		"--color=never",
		"--hidden",
		"--no-require-git",
		"--glob=!.git/**",
		"--no-messages",
	}
	if args.Glob != "" {
		arguments = append(arguments, "--glob="+args.Glob)
	}
	arguments = append(arguments, "--regexp="+args.Pattern, "--", name)

	command := exec.CommandContext(commandContext, "rg", arguments...)
	command.Dir = root.Name()

	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("could not read grep output: %w", err)
	}

	var stderr bytes.Buffer
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return "", fmt.Errorf("could not start grep: %w", err)
	}

	matches, truncated, readErr := readMatches(stdout, name == ".")
	if truncated {
		stop()
	}

	waitErr := command.Wait()

	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if readErr != nil {
		return "", fmt.Errorf("could not read grep output: %w", readErr)
	}
	if truncated {
		return util.Report(matches, true), nil
	}
	if waitErr != nil {
		var exitError *exec.ExitError
		if errors.As(waitErr, &exitError) && exitError.ExitCode() == 1 {
			return util.Report(nil, false), nil
		}

		message := strings.TrimSpace(stderr.String())
		if strings.HasPrefix(message, "rg: regex parse error:") {
			return "", fmt.Errorf("invalid pattern: %s", strings.TrimPrefix(message, "rg: "))
		}
		if message != "" {
			return "", errors.New(message)
		}

		return "", fmt.Errorf("grep failed: %w", waitErr)
	}

	return util.Report(matches, false), nil
}

func readMatches(reader io.Reader, trimWorkingDirectory bool) ([]string, bool, error) {
	bufferedReader := bufio.NewReader(reader)
	matches := make([]string, 0, util.MaxMatches)

	for {
		line, err := bufferedReader.ReadString('\n')
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			if trimWorkingDirectory {
				line = strings.TrimPrefix(line, "./")
			}

			if len(matches) == util.MaxMatches {
				return matches, true, nil
			}
			matches = append(matches, line)
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return matches, false, nil
			}

			return nil, false, err
		}
	}
}
