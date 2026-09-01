package notification

import (
	"context"

	"crdx.org/io/cmd/oh/work"
	"crdx.org/io/toolbox/notify"
)

func SendTurnError(
	ctx context.Context,
	writeEscape notify.EscapeWriter,
	workspace *work.Space,
	failure error,
) error {
	return notify.Send(ctx, writeEscape, notify.Args{
		Title:   "oh — " + workspace.GetName(),
		Message: failure.Error(),
		Icon:    "error",
	})
}
