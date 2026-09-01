package call

import (
	"path/filepath"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/internal/util/pathutil"
)

func shortenPaths(rendering agent.FallbackRendering, workspace *work.Space) agent.FallbackRendering {
	rendering.Subject = shortenPathPrefix(rendering.Subject, workspace)
	rendering.Note = shortenPathPrefix(rendering.Note, workspace)
	rendering.Emphasis.Source = shortenPathPrefix(rendering.Emphasis.Source, workspace)
	return rendering
}

func shortenPathPrefix(value string, workspace *work.Space) string {
	if workspaceDir := workspace.GetDir(); workspaceDir != "" {
		for _, prefix := range []string{workspaceDir, workspace.GetShortDir()} {
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
