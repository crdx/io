# io

**io** is a collection of agent and world-building primitives written in Go.

## Packages

- `agent`: conversation loop, with streaming, batching, and cancellation
- `tool`: tools, schemas, middleware, and concurrency
- `toolbox`: implementation of read/ls/find/grep/write/edit/bash
- `session`: session saving and resumption, as an append-only journal
- `provider/codex`: the Responses API, the OpenAI way
- `provider/chat`: the OpenAI-compatible Chat Completions API
- `provider/anthropic`: the Messages API, the Anthropic way
- ... and more, soon

## Installation

```sh
go get crdx.org/io
```

## Authentication

Implementors glue the auth flow together themselves. The example is a reference, and a way to get started quickly.

```bash
go run ./cmd/ohctl login codex
go run ./cmd/ohctl login opencode-go
go run ./cmd/ohctl login anthropic
```

## Usage

```go
type WeatherParams struct {
    City string
}

weather := tool.Define(
    "weather",
    "report weather in a city",
    tool.Schema{tool.String("city", "the city to look up")},
    func(args WeatherParams) (string, string) { return args.City, "" },
    func(_ context.Context, _ WeatherParams) (string, error) {
        return "Take a guess.", nil
    },
)

assistant := agent.New("You are a helpful weatherperson", codex.Auth(), []tool.Tool{weather})

answer, _ := assistant.Send(ctx, "what is the weather in London?")
fmt.Println(answer) // => "It's probably raining."
```

`Send` blocks until the turn is over. Cancelling the context ends the request the turn is waiting on.

`Stream` is the same turn, but an event at a time.

```go
for event, err := range assistant.Stream(ctx, "what is the weather in London?") {
    if err != nil { return err }

    switch event.Kind {
    case agent.Text:
        fmt.Print(event.Text)
    case agent.Call:
        fmt.Println("agent.Call", event.Name, event.Arguments)
    case agent.Result:
        fmt.Println("agent.Result", event.Text)
    }
}
```

`Send` is `Stream` with the text fragments glued back together, which `agent.Coalesce` does on its own.

## Implementations

### oh

A coding harness.

```bash
go run ./cmd/oh
```

### ohs

Choose and resume an `oh` session.

```bash
go run ./cmd/ohs
```

### simulate

Stand in for every provider endpoint at once, playing scenarios defined in TOML files.

```bash
go run ./cmd/simulate --scenario internal/sim/scenarios/success.toml
```

The simulator deals in wire formats, not providers. A provider speaks one of them, and more than one provider can speak the same one.

| Wire format      | Served at              | Spoken by     |
|------------------|------------------------|---------------|
| Responses        | `/v1/codex/responses`  | `codex`       |
| Chat Completions | `/v1/chat/completions` | `opencode-go` |
| Messages         | `/v1/messages`         | `anthropic`   |

## Examples

| Command         | Shows                                             |
|-----------------|---------------------------------------------------|
| `cmd/weather`   | A tool that answers questions about the weather   |
| `cmd/streaming` | A prompt loop printing each event as it arrives   |
| `cmd/simple`    | The same loop, with text fragments glued together |
| `cmd/ohctl`     | The OAuth handshake and credential storage        |

## Contributions

Open an [issue](https://github.com/crdx/io/issues) or send a [pull request](https://github.com/crdx/io/pulls).

## Licence

[GPLv3](LICENCE).
