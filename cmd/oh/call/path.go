package call

import (
	"path/filepath"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/internal/util/pathutil"
)

func shortenPaths(shown agent.FallbackRendering, workspaceDir string) agent.FallbackRendering {
	shown.Subject = shortenPathPrefix(shown.Subject, workspaceDir)
	shown.Note = shortenPathPrefix(shown.Note, workspaceDir)
	shown.Emphasis.Source = shortenPathPrefix(shown.Emphasis.Source, workspaceDir)
	return shown
}

func shortenPathPrefix(value string, workspaceDir string) string {
	if workspaceDir != "" {
		for _, prefix := range []string{workspaceDir, pathutil.Shorten(workspaceDir)} {
			rest, hasPrefix := strings.CutPrefix(value, prefix)
			switch {
			case !hasPrefix:
				continue
			case rest == "":
				return ""
			case strings.HasPrefix(rest, string(filepath.Separator)):
				return strings.TrimPrefix(rest, string(filepath.Separator))
			case strings.HasPrefix(rest, " "):
				return strings.TrimPrefix(rest, " ")
			}
		}
	}
	return pathutil.Shorten(value)
}
