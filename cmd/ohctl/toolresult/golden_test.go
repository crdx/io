package toolresult

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/agent"
	internaltoolresult "crdx.org/io/internal/toolresult"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/toolbox/bash"
	"crdx.org/io/toolbox/edit"
	"crdx.org/io/toolbox/find"
	"crdx.org/io/toolbox/grep"
	"crdx.org/io/toolbox/ls"
	"crdx.org/io/toolbox/notify"
	"crdx.org/io/toolbox/read"
	"crdx.org/io/toolbox/title"
	"crdx.org/io/toolbox/web"
	"crdx.org/io/toolbox/write"
)

func TestUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "ohctl"))
}

func TestToolResultsRenderForTheUser(t *testing.T) {
	cases := []struct {
		name     string
		exchange internaltoolresult.Exchange
	}{
		{
			name: "read source range",
			exchange: resultExchange("read", read.Args{Path: "cmd/main.go", Offset: 8, Limit: 4}, agent.SuccessStatus,
				"package main\n\nfunc main() {\n\trun()\n}\n"),
		},
		{
			name: "read image",
			exchange: resultExchange("read", read.Args{Path: "shot.png"}, agent.SuccessStatus,
				"image/png image (48217 bytes)"),
		},
		{
			name: "read failure",
			exchange: resultExchange("read", read.Args{Path: "missing.go"}, agent.ErrorStatus,
				"missing.go: no such file or directory"),
		},
		{
			name: "write file",
			exchange: resultExchange("write", write.Args{Path: "config.yaml", Content: "name: dove\nenabled: true\n"}, agent.SuccessStatus,
				"wrote 25B to config.yaml"),
		},
		{
			name: "write failure",
			exchange: resultExchange("write", write.Args{Path: "config.yaml", Content: "enabled: false\n"}, agent.ErrorStatus,
				"the filesystem is read-only"),
		},
		{
			name: "edit with context",
			exchange: resultExchange("edit", edit.Args{
				Path:    "main.go",
				OldText: "one\ntwo\nthree\nfour\noldCall()\nsix\nseven\neight\nnine\n",
				NewText: "one\ntwo\nthree\nfour\nnewCall()\nsix\nseven\neight\nnine\n",
			}, agent.SuccessStatus, "edited main.go"),
		},
		{
			name: "edit failure",
			exchange: resultExchange("edit", edit.Args{Path: "main.go", OldText: "old", NewText: "new"}, agent.ErrorStatus,
				"old_text does not appear in the file"),
		},
		{
			name: "shell success",
			exchange: resultExchange("bash", bash.Args{Command: "go test ./...\nprintf 'done\\n'"}, agent.SuccessStatus,
				"ok  crdx.org/io\ndone\n"),
		},
		{
			name: "shell failure",
			exchange: resultExchange("bash", bash.Args{Command: "just check"}, agent.ErrorStatus,
				"lint1  ✗\nmain.go:12: undefined: run\n"),
		},
		{
			name:     "shell without output",
			exchange: resultExchange("bash", bash.Args{Command: "true"}, agent.SuccessStatus, ""),
		},
		{
			name: "shell cancelled",
			exchange: resultExchange("bash", bash.Args{Command: "sleep 30"}, agent.CancelledStatus,
				"the command was stopped because the user pressed escape"),
		},
		{
			name: "list directory",
			exchange: resultExchange("ls", ls.Args{Path: "cmd"}, agent.SuccessStatus,
				"oh/\nohctl/\nsimple/\n"),
		},
		{
			name:     "empty directory",
			exchange: resultExchange("ls", ls.Args{Path: "empty"}, agent.SuccessStatus, "(empty)"),
		},
		{
			name: "find files",
			exchange: resultExchange("find", find.Args{Pattern: "**/*.go", Path: "cmd"}, agent.SuccessStatus,
				"cmd/oh/main.go\ncmd/ohctl/main.go\n"),
		},
		{
			name:     "find without matches",
			exchange: resultExchange("find", find.Args{Pattern: "**/*.cobol"}, agent.SuccessStatus, "(no matches)"),
		},
		{
			name: "grep source",
			exchange: resultExchange("grep", grep.Args{Pattern: "func (Run|Open)", Path: "cmd", Glob: "**/*.go"}, agent.SuccessStatus,
				"cmd/oh/main.go:12:func Run() error {\ncmd/ohctl/open.go:8:func Open() {\n"),
		},
		{
			name: "grep failure",
			exchange: resultExchange("grep", grep.Args{Pattern: "[", Path: "cmd"}, agent.ErrorStatus,
				"regex parse error: unclosed character class"),
		},
		{
			name: "web search",
			exchange: resultExchange("web_search", web.SearchArgs{Query: "modern Go release"}, agent.SuccessStatus,
				"## Result\n\nGo has a new release. [Source](https://example.test/release)."),
		},
		{
			name: "web fetch markdown",
			exchange: resultExchange("web_fetch", web.FetchArgs{URL: "https://example.test/article", Type: "markdown"}, agent.SuccessStatus,
				"# Article\n\n- first\n- second\n"),
		},
		{
			name: "web fetch html",
			exchange: resultExchange("web_fetch", web.FetchArgs{URL: "https://example.test/raw", Type: "raw"}, agent.SuccessStatus,
				"<!DOCTYPE html>\n<title>Hello</title>\n"),
		},
		{
			name: "web fetch failure",
			exchange: resultExchange("web_fetch", web.FetchArgs{URL: "https://example.test/missing", Type: "text"}, agent.ErrorStatus,
				"web fetch failed with status 404: page not found"),
		},
		{
			name: "title",
			exchange: resultExchange("title", title.Args{Title: "render-useful-results"}, agent.SuccessStatus,
				"the session is now titled \"render-useful-results\""),
		},
		{
			name: "notification",
			exchange: resultExchange("notify", notify.Args{Title: "Checks passed", Message: "Everything is green", Icon: "success"}, agent.SuccessStatus,
				"notified the user"),
		},
		{
			name: "unknown tool",
			exchange: resultExchange("weather", map[string]any{"city": "London", "days": 3}, agent.SuccessStatus,
				"Rain, then sun."),
		},
	}

	var drawn strings.Builder
	for _, test := range cases {
		fmt.Fprintf(&drawn, "=== %s ===\n%s\n\n", test.name, strutil.VisibleEscapes(render(test.exchange, 60)))
	}
	assertGolden(t, "render.ansi", drawn.String())
}

func resultExchange(name string, arguments any, status agent.Status, text string) internaltoolresult.Exchange {
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		panic(err)
	}
	return internaltoolresult.Exchange{
		Request: agent.Event{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: name, Arguments: string(encodedArguments)},
		Result:  agent.Event{Kind: agent.ToolCallResultEvent, ID: "call-1", Name: name, Status: status, Text: text},
	}
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
