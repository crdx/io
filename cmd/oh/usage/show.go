package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"crdx.org/io/cmd/oh/backend"
	"crdx.org/io/cmd/oh/graphics"
	"crdx.org/io/cmd/oh/location"
)

func Show(ctx context.Context, output io.Writer, options Options) error {
	report := Collect(ctx, StoredSources(), time.Now)

	if options.JSON {
		document, err := json.Marshal(report)
		if err != nil {
			return err
		}

		_, err = fmt.Fprintln(output, string(document))

		return err
	}

	var drawing *Graphics
	if cellWidth, cellHeight, hasGraphics := graphics.Detect(os.Stdin, os.Stdout); hasGraphics {
		drawing = &Graphics{CellWidth: cellWidth, CellHeight: cellHeight}
	}

	_, err := io.WriteString(output, Render(report, time.Now(), drawing))

	return err
}

func StoredSources() []Source {
	usageSources := backend.UsageSources()
	sources := make([]Source, 0, len(usageSources))

	for _, source := range usageSources {
		sources = append(sources, Source{
			Provider:             source.Provider,
			Label:                source.Label,
			Reporter:             source.Reporter,
			CachePath:            location.GetUsageCachePath(source.Provider, false),
			HasIdleSessionWindow: source.HasIdleSessionWindow,
			IsSelfRefreshing:     source.Reporter != nil,
		})
	}

	return sources
}
