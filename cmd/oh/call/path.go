package call

import (
	"path/filepath"
	"slices"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/internal/util/pathutil"
	"crdx.org/io/tool"
)

func shortenPaths(rendering agent.FallbackRendering, workspace *work.Space) agent.FallbackRendering {
	primary := shortenCallRendering(tool.CallRendering{
		Subject:   rendering.Subject,
		Qualifier: rendering.Note,
		Emphasis:  rendering.Emphasis,
	}, workspace)
	rendering.Subject = primary.Subject
	rendering.Note = primary.Qualifier
	rendering.Emphasis = primary.Emphasis
	rendering.Continuation = slices.Clone(rendering.Continuation)
	for i := range rendering.Continuation {
		rendering.Continuation[i] = shortenCallRendering(rendering.Continuation[i], workspace)
	}
	return rendering
}

func shortenCallRendering(rendering tool.CallRendering, workspace *work.Space) tool.CallRendering {
	rendering.Subject = shortenPathPrefix(rendering.Subject, workspace)
	rendering.Qualifier = shortenPathPrefix(rendering.Qualifier, workspace)
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
