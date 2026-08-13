package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"crdx.org/io/agent"
	"crdx.org/io/codex"
	"crdx.org/io/tool"
)

func main() {
	assistant := agent.New("You are a helpful assistant.", codex.Auth(), []tool.Tool{})
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
	for event, err := range assistant.Stream(prompt) {
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
