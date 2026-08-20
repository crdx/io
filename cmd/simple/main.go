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

func answer(assistant *agent.Agent, prompt string) {
	for event, err := range agent.Coalesce(assistant.Stream(context.Background(), prompt)) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "\n"+err.Error())
			return
		}

		switch event.Kind {
		case agent.Text:
			fmt.Print(event.Text)
		case agent.Call:
			fmt.Printf("\n· %s %s\n", event.Name, event.Arguments)
		case agent.Result:
			fmt.Printf("← %s\n", event.Text)
		}
	}

	fmt.Println()
}
