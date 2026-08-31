package pathGrants

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"crdx.org/io/cmd/oh/pathgrant"
	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
	"crdx.org/io/internal/util/pathutil"
)

const (
	base  = "base"
	short = "short"
	full  = "full"
)

type state struct {
	getGrants func() []pathgrant.Grant
	pathType  string
}

func New(getGrants func() []pathgrant.Grant) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Type string `toml:"type"`
		}
		if err := options.Read(&args); err != nil {
			return nil, err
		}
		switch args.Type {
		case "", base, short, full:
		default:
			return nil, fmt.Errorf(
				"type is %q, and wants to be omitted or %q, %q, or %q",
				args.Type,
				base,
				short,
				full,
			)
		}
		return state{getGrants: getGrants, pathType: args.Type}, nil
	}
}

func (self state) Render(segment.Context) string {
	return strings.Join(self.getParts(), style.Subtle(", "))
}

func (self state) RenderWithin(_ segment.Context, cells int) string {
	parts := self.getParts()
	if len(parts) == 0 || cells <= 0 {
		return ""
	}

	all := strings.Join(parts, style.Subtle(", "))
	if style.Width(all) <= cells {
		return all
	}

	for shownCount := range slices.Backward(parts) {
		hiddenCount := len(parts) - shownCount
		candidateParts := append([]string(nil), parts[:shownCount]...)
		candidateParts = append(candidateParts, style.Subtle("+"+strconv.Itoa(hiddenCount)))
		candidate := strings.Join(candidateParts, style.Subtle(", "))
		if style.Width(candidate) <= cells {
			return candidate
		}
	}

	if cells == 1 {
		return style.Subtle("+")
	}
	return style.Subtle(width.Elide("+"+strconv.Itoa(len(parts)), cells))
}

func (self state) getParts() []string {
	grants := self.getGrants()
	baseCounts := make(map[string]int, len(grants))
	for _, grant := range grants {
		baseCounts[filepath.Base(grant.Path)]++
	}

	parts := make([]string, 0, len(grants))
	for _, grant := range grants {
		pathType := self.pathType
		if (pathType == "" || pathType == base) && baseCounts[filepath.Base(grant.Path)] > 1 {
			pathType = short
		}
		parts = append(parts, renderGrant(grant, pathType))
	}
	return parts
}

func renderGrant(grant pathgrant.Grant, pathType string) string {
	path := grant.Path
	switch pathType {
	case "", base:
		path = filepath.Base(path)
	case short:
		path = pathutil.Shorten(path)
	case full:
	}

	access := style.Read("r")
	if grant.Access == pathgrant.WriteAccess {
		access = style.Write("w")
	}
	return access + style.Subtle(":") + style.Normal(path)
}
