package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testHTML = `<!DOCTYPE html>
<html>
<head><style>hidden { color: red }</style></head>
<body>
<nav>Menu</nav>
<h1>Example &amp; test</h1>
<p>Read <strong>this</strong> <a href="https://example.com/more">page</a>.</p>
<ul><li>First</li><li>Second</li></ul>
<script>doBadThings()</script>
</body>
</html>`

func TestFetchReturnsEverySupportedFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "text/html, application/xhtml+xml" {
			t.Errorf("got accept header %q", request.Header.Get("Accept"))
		}
		_, _ = writer.Write([]byte(testHTML))
	}))
	defer server.Close()

	for format, want := range map[string][]string{
		"raw":        {"<!DOCTYPE html>", "doBadThings()"},
		"clean_html": {"<h1>Example &amp; test</h1>", "<nav>Menu</nav>"},
		"text":       {"Example & test", "Read this page.", "First", "Second"},
		"markdown":   {"# Example & test", "**this**", "[page](https://example.com/more)", "- First"},
	} {
		got, err := fetchPage(t.Context(), server.Client(), FetchArgs{URL: server.URL, Type: format})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		for _, fragment := range want {
			if !strings.Contains(got, fragment) {
				t.Errorf("%s: expected %q in %q", format, fragment, got)
			}
		}
		if format != "raw" && strings.Contains(got, "doBadThings") {
			t.Errorf("%s retained stripped script: %q", format, got)
		}
	}
}

func TestFetchParsesMalformedHTMLAsADocumentTree(t *testing.T) {
	page := `<body><p data-note="1 > 0">one <b>two<p>three<script>bad()`
	cleanHTML := fetchTestPage(t, page, "clean_html")

	for _, want := range []string{`data-note="1 &gt; 0"`, "<b>two</b>", "<p><b>three</b></p>"} {
		if !strings.Contains(cleanHTML, want) {
			t.Errorf("expected %q in %q", want, cleanHTML)
		}
	}
	if strings.Contains(cleanHTML, "bad()") {
		t.Errorf("script survived in %q", cleanHTML)
	}

	text := fetchTestPage(t, page, "text")
	if text != "one two\nthree" {
		t.Errorf("got text %q", text)
	}
}

func fetchTestPage(t *testing.T, page string, format string) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(page))
	}))
	defer server.Close()

	output, err := fetchPage(t.Context(), server.Client(), FetchArgs{URL: server.URL, Type: format})
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func TestFetchRejectsInvalidURLsAndFormats(t *testing.T) {
	for _, args := range []FetchArgs{
		{URL: "relative", Type: "text"},
		{URL: "https://example.com", Type: "pdf"},
	} {
		if err := validateFetch(args); err == nil {
			t.Errorf("expected %#v to be rejected", args)
		}
	}
}

func TestFetchReportsHTTPFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
		_, _ = writer.Write([]byte("not today"))
	}))
	defer server.Close()

	_, err := fetchPage(t.Context(), server.Client(), FetchArgs{URL: server.URL, Type: "text"})
	if err == nil || !strings.Contains(err.Error(), "status 418: not today") {
		t.Errorf("got %v", err)
	}
}
