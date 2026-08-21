package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/provider/codex"
	"crdx.org/io/tool"
)

func main() {
	client, err := codex.Auth("gpt-5.6-sol", "high")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	assistant := agent.New("You are a helpful assistant.", client, []tool.Tool{})
	input := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("> ")

		line, err := input.ReadString('\n')
		if err != nil {
			fmt.Println()
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		answer(assistant, line)
	}
}

func answer(assistant *agent.Agent, message string) {
	for event, err := range assistant.Stream(context.Background(), message) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "\n"+err.Error())
			return
		}

		switch event.Kind {
		case agent.ModelMessage:
			fmt.Print(event.Text)
		case agent.ToolCallRequest:
			fmt.Printf("\n· %s %s\n", event.Name, event.Arguments)
		case agent.ToolCallResult:
			fmt.Printf("← %s\n", event.Text)
		}
	}

	fmt.Println()
}
