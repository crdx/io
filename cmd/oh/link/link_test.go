package link

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestURLLinkKeepsTheCompleteAddressVisible(t *testing.T) {
	address := "https://example.test/authorise?token=one"
	got := RenderURL(address, address)
	want := "\x1b]8;;" + address + "\x1b\\" + address + "\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if Plain(got) != address {
		t.Errorf("visible address is %q", Plain(got))
	}
}

func TestWebURLsAreLinkedOnlyForSupportedSchemes(t *testing.T) {
	for name, test := range map[string]struct {
		address      string
		shouldRender bool
	}{
		"http":             {address: "http://example.test/path", shouldRender: true},
		"https":            {address: "https://example.test/path", shouldRender: true},
		"email":            {address: "mailto:person@example.test", shouldRender: true},
		"encoded space":    {address: "https://example.test/a b", shouldRender: true},
		"relative":         {address: "docs/page.html"},
		"missing host":     {address: "https:path"},
		"unsupported":      {address: "javascript:alert(1)"},
		"terminal control": {address: "https://example.test/\x1b]8;;bad"},
	} {
		got := RenderWebURL("label", test.address)
		wasRendered := got != "label"
		if wasRendered != test.shouldRender {
			t.Errorf("%s: rendered=%t: %q", name, wasRendered, got)
		}
		if Plain(got) != "label" {
			t.Errorf("%s: visible label = %q", name, Plain(got))
		}
	}
}

func TestSourceLocationsBecomeFileFragmentsWithoutChangingTheirText(t *testing.T) {
	workspace := t.TempDir()
	path := prepareFile(t, workspace, "cmd/oh/draw.go")

	for _, test := range []struct {
		location string
		fragment string
	}{
		{location: "cmd/oh/draw.go:42", fragment: "42"},
		{location: "cmd/oh/draw.go:42:7", fragment: "42:7"},
	} {
		got := Render("see "+test.location+" and carry on", workspace)
		address := linkAddress(t, got)

		if address.Scheme != "file" || address.Path != filepath.ToSlash(path) || address.Fragment != test.fragment {
			t.Errorf("got address %q", address)
		}
		if stripEscapes(got) != "see "+test.location+" and carry on" {
			t.Errorf("expected the visible text unchanged, got %q", stripEscapes(got))
		}
	}
}

func TestAPathAlreadyLinkedToTheWebIsNotNestedInAFileLink(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "cmd/oh/draw.go")
	linkedPath := RenderWebURL("cmd/oh/draw.go", "https://example.test/source")

	if got := Render("read "+linkedPath, workspace); got != "read "+linkedPath {
		t.Errorf("already-linked path changed: %q", got)
	}
}

func TestAPathSplitAcrossStylesBecomesOneLink(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "cmd/oh/draw.go")
	styledPath := "\x1b[2mcmd/oh/\x1b[0m\x1b[1mdraw.go\x1b[0m"

	got := Render("read "+styledPath, workspace)

	if strings.Count(got, openPrefix) != 2 {
		t.Errorf("expected one opening and one closing sequence, got %q", got)
	}
	if withoutLinks := stripHyperlinks(got); withoutLinks != "read "+styledPath {
		t.Errorf("expected the styles within the path unchanged, got %q", withoutLinks)
	}
	if stripEscapes(got) != "read cmd/oh/draw.go" {
		t.Errorf("expected the visible text unchanged, got %q", stripEscapes(got))
	}
}

func TestAbsoluteDirectoriesAndSpecialURLCharacters(t *testing.T) {
	workspace := t.TempDir()
	path := prepareFile(t, workspace, "100%.txt")

	got := Render(workspace+" "+filepath.Base(path), workspace)

	if strings.Count(got, openPrefix) != 4 {
		t.Errorf("expected two links, got %q", got)
	}
	if !strings.Contains(got, "100%25.txt") {
		t.Errorf("expected an escaped URL, got %q", got)
	}
}

func TestMultiDotAndHiddenFilenamesBecomeLinks(t *testing.T) {
	workspace := t.TempDir()
	prepareFile(t, workspace, "AGENTS.local.md")
	prepareFile(t, workspace, ".gitignore")

	got := Render("AGENTS.local.md and .gitignore", workspace)

	if strings.Count(got, openPrefix) != 4 {
		t.Errorf("expected two links, got %q", got)
	}
	if stripEscapes(got) != "AGENTS.local.md and .gitignore" {
		t.Errorf("expected filenames unchanged, got %q", stripEscapes(got))
	}
}

func TestMissingPathsAndOrdinaryDottedWordsStayPlain(t *testing.T) {
	text := "missing.go and example.com are not files here"

	if got := Render(text, t.TempDir()); got != text {
		t.Errorf("got %q, want unchanged text", got)
	}
}

func linkAddress(t *testing.T, rendered string) *url.URL {
	t.Helper()

	begin := strings.Index(rendered, openPrefix)
	if begin < 0 {
		t.Fatalf("no hyperlink in %q", rendered)
	}

	end := strings.Index(rendered[begin+len(openPrefix):], terminator)
	if end < 0 {
		t.Fatalf("unterminated hyperlink in %q", rendered)
	}
	end += begin + len(openPrefix)

	address, err := url.Parse(rendered[begin+len(openPrefix) : end])
	if err != nil {
		t.Fatalf("parse hyperlink: %v", err)
	}
	return address
}

func prepareFile(t *testing.T, workspace string, name string) string {
	t.Helper()

	path := filepath.Join(workspace, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("prepare directory: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("prepare file: %v", err)
	}
	return path
}

func stripHyperlinks(text string) string {
	text = strings.ReplaceAll(text, closeLink, "")
	for {
		begin := strings.Index(text, openPrefix)
		if begin < 0 {
			return text
		}
		end := strings.Index(text[begin:], terminator)
		if end < 0 {
			return text
		}
		text = text[:begin] + text[begin+end+len(terminator):]
	}
}

func stripEscapes(text string) string {
	var plain strings.Builder

	for i := 0; i < len(text); {
		if text[i] == '\x1b' {
			i = escapeEnd(text, i)
			continue
		}
		plain.WriteByte(text[i])
		i++
	}

	return plain.String()
}
