package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/store"
	"crdx.org/io/tool"
)

const (
	thinkingFor = 2 * time.Second
	wordEvery   = 120 * time.Millisecond
	toolTakes   = 3 * time.Second
)

type fakeProvider struct {
	turn  int
	items []json.RawMessage // the stored provider state
}

func (self *fakeProvider) Configure(string, []tool.Definition) {}
func (self *fakeProvider) AddUserMessage(string)               {}
func (self *fakeProvider) AddToolResults([]agent.ToolResult)   {}
func (self *fakeProvider) Dump() []json.RawMessage             { return self.items }
func (self *fakeProvider) Load(items []json.RawMessage)        { self.items = items }

func (self *fakeProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.turn++

	time.Sleep(thinkingFor)

	for _, thought := range []string{
		"**Reading the file**\nThe path they gave is the one to start with.",
		"**Searching for the spinner**\nIt is drawn somewhere near the output layer.",
	} {
		if !yield(agent.Event{Kind: agent.Reasoning, Text: thought}) {
			return agent.Reply{}, nil
		}

		time.Sleep(wordEvery)
	}

	for word := range strings.FieldsSeq("let me have a look at that for you") {
		if !yield(agent.Event{Kind: agent.Text, Text: word + " "}) {
			return agent.Reply{}, nil
		}

		time.Sleep(wordEvery)
	}

	if self.turn > 1 {
		return agent.Reply{}, nil
	}

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "1", Name: "read", Arguments: `{"path":"main.go"}`},
		{ID: "2", Name: "grep", Arguments: `{"path":"spinner"}`},
		{ID: "3", Name: "write", Arguments: `{"path":"notes.md"}`},
	}}, nil
}

type fakeArgs struct {
	Path string `json:"path"`
}

func slowTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        name,
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, "" },
	).Plain(func(context.Context, fakeArgs) (string, error) {
		time.Sleep(toolTakes)
		return "one\ntwo\nthree", nil
	})
}

func slowReadTool(name string) tool.Tool {
	return tool.ReadOnly(tool.Concurrent(slowTool(name)))
}

func failingTool(name string) tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        name,
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, "" },
	).Plain(func(context.Context, fakeArgs) (string, error) {
		time.Sleep(toolTakes)
		return "", errors.New("permission denied\nnothing was written")
	})
}

func TestVisual(t *testing.T) {
	if os.Getenv("RIG") == "" {
		t.Skip("set RIG to watch it draw")
	}

	tools := []tool.Tool{slowReadTool("read"), slowReadTool("grep"), failingTool("write")}
	provider := &fakeProvider{}
	screen := output.New(os.Stdout)

	log, err := store.Create(t.TempDir(), store.Meta{Model: "fake"})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = log.Close() }()

	held := &conversation{
		assistant: agent.New("", provider, tools),
		screen:    screen,
		log:       log,
		mode:      NewMode(capRead | capWrite),
		label: func(pending bool, frame int, running bool) string {
			return banner(
				"fake", "medium", "/tmp/somewhere", tools, true, false, false,
				pending, frame, running,
			)
		},
	}

	held.makeIntroductions("")
}
