package exposedPorts

import (
	"slices"
	"strconv"
	"strings"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/style"
	"crdx.org/io/cmd/oh/width"
)

const (
	hostToSandboxArrow = "⇢ "
	sandboxToHostArrow = "⇠ "
)

type state struct {
	getHostToSandbox func() []uint16
	getSandboxToHost func() []uint16
}

func New(
	getHostToSandbox func() []uint16,
	getSandboxToHost func() []uint16,
) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}
		return state{
			getHostToSandbox: getHostToSandbox,
			getSandboxToHost: getSandboxToHost,
		}, nil
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
	hostToSandbox := self.getHostToSandbox()
	sandboxToHost := self.getSandboxToHost()
	parts := make([]string, 0, len(hostToSandbox)+len(sandboxToHost))
	parts = appendParts(parts, hostToSandboxArrow, hostToSandbox)
	return appendParts(parts, sandboxToHostArrow, sandboxToHost)
}

func appendParts(parts []string, arrow string, ports []uint16) []string {
	for _, port := range ports {
		parts = append(parts, style.Subtle(arrow)+style.Normal(strconv.Itoa(int(port))))
	}
	return parts
}
