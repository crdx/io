package main

import (
	"fmt"

	"crdx.org/io/agent"
	"crdx.org/io/provider/codex"
	"crdx.org/io/tool"
)

func main() {
	type WeatherParams struct {
		City string
	}

	weather := tool.Define(
		"weather",
		"report weather in a city",
		tool.Schema{tool.String("city", "the city to look up")},
		func(args WeatherParams) string { return args.City },
		func(args WeatherParams) (string, error) {
			return "Cloudy with a chance of meatballs.", nil
		},
	)

	assistant := agent.New(
		"You are a helpful weatherperson",
		codex.Auth(),
		[]tool.Tool{weather},
	)

	answer, _ := assistant.Send("what is the weather in London?")
	fmt.Println(answer) // => "London is cloudy, with a chance of meatballs."
}
