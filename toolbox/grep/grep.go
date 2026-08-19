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
	definedTool := tool.Implement(
		tool.Definition{
			Name:        "grep",
			Description: "search file contents",
			Schema: tool.Schema{
				tool.String("pattern", "regexp"),
				tool.String("path", "directory, defaults to working directory").Optional(),
				tool.String("glob", "path filter, e.g. **/*.go").Optional(),
			},
		},
		Render,
	).Measured(func(ctx context.Context, args Args) (string, tool.Statistics, error) {
		return run(ctx, root, args)
	})

	return tool.ReadOnly(tool.Concurrent(tool.Focus(definedTool, util.SearchPath)))
}

// Render describes the search out loud.
func Render(args Args) (string, string) {
	return util.RenderSearch(args.Pattern, args.Path, args.Glob)
}

func run(ctx context.Context, root *file.Root, args Args) (string, tool.Statistics, error) {
	if args.Pattern == "" {
		return "", tool.Statistics{}, errors.New("pattern is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.Statistics{}, err
	}

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

	searchContext, stopSearch := context.WithCancel(ctx)
	defer stopSearch()

	command := exec.CommandContext(searchContext, "rg", arguments...)
	command.Dir = root.Name()

	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", tool.Statistics{}, fmt.Errorf("could not read ripgrep output: %w", err)
	}

	var stderr bytes.Buffer
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return "", tool.Statistics{}, fmt.Errorf("could not start ripgrep: %w", err)
	}

	matches, returnedBytes, truncated, readErr := readMatches(stdout, name == ".")
	if truncated {
		stopSearch()
	}

	waitErr := command.Wait()

	if ctx.Err() != nil {
		return "", tool.Statistics{}, ctx.Err()
	}
	if readErr != nil {
		return "", tool.Statistics{}, fmt.Errorf("could not read ripgrep output: %w", readErr)
	}

	if truncated {
		output, stats := searchReport(matches, returnedBytes, true)
		return output, stats, nil
	}
	if waitErr != nil {
		var exitError *exec.ExitError
		if errors.As(waitErr, &exitError) && exitError.ExitCode() == 1 {
			output, stats := searchReport(nil, 0, false)
			return output, stats, nil
		}

		message := strings.TrimSpace(stderr.String())
		if strings.HasPrefix(message, "rg: regex parse error:") {
			return "", tool.Statistics{}, fmt.Errorf("invalid pattern: %s", strings.TrimPrefix(message, "rg: "))
		}
		if message != "" {
			return "", tool.Statistics{}, errors.New(message)
		}

		return "", tool.Statistics{}, fmt.Errorf("grep failed: %w", waitErr)
	}

	output, stats := searchReport(matches, returnedBytes, false)
	return output, stats, nil
}

func searchReport(matches []string, returnedBytes int64, truncated bool) (string, tool.Statistics) {
	output := util.ReportSearchResults(matches, truncated)
	totalBytes := returnedBytes
	if truncated {
		totalBytes = 0
	}

	return output, tool.Statistics{
		Kind:       tool.StatsSearch,
		Lines:      int64(len(matches)),
		Bytes:      returnedBytes,
		TotalBytes: totalBytes,
		Truncated:  truncated,
	}
}

func readMatches(reader io.Reader, trimWorkingDirectory bool) ([]string, int64, bool, error) {
	bufferedReader := bufio.NewReader(reader)
	var matches []string
	returnedBytes := int64(0)

	for {
		line, err := bufferedReader.ReadString('\n')
		if line != "" {
			line = strings.TrimSuffix(line, "\n")
			if trimWorkingDirectory {
				line = strings.TrimPrefix(line, "./")
			}

			var truncated bool
			matches, returnedBytes, truncated = util.AppendSearchResult(matches, returnedBytes, line)
			if truncated {
				return matches, returnedBytes, true, nil
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return matches, returnedBytes, false, nil
			}

			return nil, 0, false, err
		}
	}
}
