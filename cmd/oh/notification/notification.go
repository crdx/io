package notification

import (
	"context"
	"path/filepath"

	"crdx.org/io/toolbox/notify"
)

func SendTurnError(ctx context.Context, writeEscape notify.EscapeWriter, workspaceDir string, failure error) error {
	return notify.Send(ctx, writeEscape, notify.Args{
		Title:   "oh — " + filepath.Base(workspaceDir),
		Message: failure.Error(),
		Icon:    "error",
	})
}
