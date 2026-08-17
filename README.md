# io

**io** is a collection of agent and world-building primitives written in Go.

## Installation

```sh
go get crdx.org/io
```

## Authentication

Implementors handle gluing together the auth flow. Use the example for a reference, or to authenticate yourself so you can get started quickly.

```bash
go run ./cmd/login
```

## Examples

### weather

A weather lookup tool.

```bash
go run ./cmd/weather
```

<details>
<summary>Expand</summary>

```go
type WeatherParams struct {
    City string
}

weather := tool.Define(
    "weather",
    "report weather in a city",
    tool.Schema{tool.String("city", "the city to look up")},
    func(args WeatherParams) string { return args.City },
    func(args WeatherParams) (string, error) {
        return "No idea. Use your best judgement.", nil
    },
)

assistant := agent.New(
    "You are a helpful weatherperson",
    codex.Auth(),
    []tool.Tool{weather},
)

answer, _ := assistant.Send(ctx, "what is the weather in London?")
fmt.Println(answer) // => "It's raining in London."
```

`Send` triggers an agent turn and blocks until completion. Cancelling the context ends the request the turn is waiting on, which is the only part of a turn that can be interrupted: a tool that has started cannot be stopped, and is waited for.

</details>

### streaming

A prompt loop that prints each event as it arrives.

```bash
go run ./cmd/streaming
```

<details>
<summary>Expand</summary>

```go
for event, err := range assistant.Stream(ctx, "what is the weather in London?") {
    if err != nil { return err }

    switch event.Kind {
    case agent.Text:
        fmt.Print(event.Value)
    case agent.Call:
        fmt.Printf("%s: %s\n", event.Name, event.Arguments)
    case agent.Result:
        fmt.Printf("← %s\n", event.Value)
    }
}
```

`Send` is `Stream` with the text fragments glued back together.

</details>

## Contributions

Open an [issue](https://github.com/crdx/io/issues) or send a [pull request](https://github.com/crdx/io/pulls).

## Licence

[GPLv3](LICENCE).
