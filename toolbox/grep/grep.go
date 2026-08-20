package grep

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
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

var matchPathPattern = regexp.MustCompile(`^(.+):[0-9]+:`)

// New builds the grep tool confined to root. A match is reported as path:line:text.
func New(root *file.Root, snapshots *file.Snapshots) tool.Tool {
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
		Describe,
	).Run(func(ctx context.Context, args Args) (tool.Result, error) {
		output, stats, err := run(ctx, root, args)
		return tool.Result{
			Output: output,
			Stats:  stats,
			State:  readStateForMatches(root, args, output),
		}, err
	})
	definedTool = tool.State(definedTool, file.FileReadState, func(payload json.RawMessage) error {
		return snapshots.RestoreReadState(root, payload)
	})

	return tool.ReadOnly(tool.Concurrent(tool.Syntax(tool.Focus(definedTool, util.SearchPath), "regexp")))
}

// Describe reports the search's subject and qualifier.
func Describe(args Args) (string, string) {
	return util.DescribeSearch(args.Pattern, args.Path, args.Glob)
}

func run(ctx context.Context, root *file.Root, args Args) (string, tool.Stats, error) {
	if args.Pattern == "" {
		return "", tool.Stats{}, errors.New("pattern is required")
	}

	root, name, err := root.Resolve(args.Path)
	if err != nil {
		return "", tool.Stats{}, err
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
		"--max-columns=250",
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
		return "", tool.Stats{}, fmt.Errorf("could not read ripgrep output: %w", err)
	}

	var stderr bytes.Buffer
	command.Stderr = &stderr

	if err := command.Start(); err != nil {
		return "", tool.Stats{}, fmt.Errorf("could not start ripgrep: %w", err)
	}

	matches, truncated, readErr := readMatches(stdout, name == ".")
	if truncated {
		stopSearch()
	}

	waitErr := command.Wait()

	if ctx.Err() != nil {
		return "", tool.Stats{}, ctx.Err()
	}
	if readErr != nil {
		return "", tool.Stats{}, fmt.Errorf("could not read ripgrep output: %w", readErr)
	}

	if truncated {
		output, stats := searchReport(matches, true)
		return output, stats, nil
	}
	if waitErr != nil {
		var exitError *exec.ExitError
		if errors.As(waitErr, &exitError) && exitError.ExitCode() == 1 {
			output, stats := searchReport(nil, false)
			return output, stats, nil
		}

		message := strings.TrimSpace(stderr.String())
		if strings.HasPrefix(message, "rg: regex parse error:") {
			return "", tool.Stats{}, fmt.Errorf("invalid pattern: %s", strings.TrimPrefix(message, "rg: "))
		}
		if message != "" {
			return "", tool.Stats{}, errors.New(message)
		}

		return "", tool.Stats{}, fmt.Errorf("grep failed: %w", waitErr)
	}

	output, stats := searchReport(matches, false)
	return output, stats, nil
}

func searchReport(matches []string, truncated bool) (string, tool.Stats) {
	output := util.ReportSearchResults(matches, truncated)
	stats := tool.OutputStats(output)
	stats.Truncated = truncated

	return output, stats
}

func readStateForMatches(root *file.Root, args Args, output string) json.RawMessage {
	searchRoot, _, err := root.Resolve(args.Path)
	if err != nil {
		return nil
	}

	seen := map[string]bool{}
	var readSnapshots []file.ReadSnapshot
	for line := range strings.Lines(output) {
		match := matchPathPattern.FindStringSubmatch(line)
		if match == nil || seen[match[1]] {
			continue
		}
		seen[match[1]] = true

		content, err := searchRoot.ReadFile(match[1])
		if err != nil {
			continue
		}

		path := match[1]
		if searchRoot != root {
			path = filepath.Join(searchRoot.Name(), path)
		}
		readSnapshots = append(readSnapshots, file.NewReadSnapshot(path, content))
	}

	if len(readSnapshots) == 0 {
		return nil
	}
	return file.EncodeReadState(readSnapshots...)
}

func readMatches(reader io.Reader, trimWorkingDirectory bool) ([]string, bool, error) {
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
				return matches, true, nil
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return matches, false, nil
			}

			return nil, false, err
		}
	}
}
