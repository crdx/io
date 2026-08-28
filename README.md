# io

**io** is a collection of agent and world-building primitives written in Go.

## Packages

- `agent`: conversation loop, with streaming, batching, and cancellation
- `tool`: tools, schemas, middleware, and concurrency
- `toolbox`: implementation of standard tools, plus extras
- `session`: session saving and resumption, as an append-only journal
- `wire/anthropic/messages`: the Anthropic Messages protocol
- `wire/openai/chatcompletions`: the OpenAI-compatible Chat Completions protocol
- `wire/openai/responses`: the OpenAI Responses protocol
- `provider/anthropic`: the Claude subscription service
- `provider/codex`: the ChatGPT Codex service
- `provider/opencodego`: the OpenCode Go service
- `provider/ollama`: local and network Ollama servers
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

`Stream` is the same turn as transient prose deltas and completed events. Deltas are suitable for live rendering; only events belong in durable history.

```go
for update, err := range assistant.Stream(ctx, "what is the weather in London?") {
    if err != nil {
        return err
    }

    switch {
    case update.Delta != nil:
        if update.Delta.Kind == agent.ModelMessageEvent {
            fmt.Print(update.Delta.Text)
        }
    case update.Event != nil && update.Event.Kind == agent.ToolCallRequestEvent:
        fmt.Println("call", update.Event.Name, update.Event.Arguments)
    case update.Event != nil && update.Event.Kind == agent.ToolCallResultEvent:
        fmt.Println("result", update.Event.Text)
    }
}
```

`Send` collects the completed `ModelMessageEvent` blocks and returns their text.

## Implementations

### oh

A coding harness. Pass `-r` without a name to choose and resume a stored session.

```bash
go run ./cmd/oh
go run ./cmd/oh -r
```

Ollama uses `http://localhost:11434` by default. After upgrading, run `go run ./cmd/ohctl migrate`, then configure a network server in `config.toml`:

```toml
[provider.ollama]
host = "http://ollama-server:11434"
```

After changing the host, refresh the model cache and select one of the installed models:

```bash
go run ./cmd/oh -u
go run ./cmd/oh -m ollama/qwen3.8:27b-mtp-q8_0-256k@high
```

`OLLAMA_HOST` temporarily overrides the configured host. `OH_ENDPOINT_URL`, used by the simulator, overrides both.

### simulate

Stand in for every provider endpoint at once, playing scenarios defined in TOML files.

```bash
go run ./cmd/simulate --scenario internal/sim/scenarios/success.toml
```

The simulator deals in wire formats, not providers. A provider speaks one of them, and more than one provider can speak the same one.

| Wire format      | Served at              | Spoken by               |
|------------------|------------------------|-------------------------|
| Responses        | `/v1/codex/responses`  | `codex`                 |
| Chat Completions | `/v1/chat/completions` | `opencode-go`, `ollama` |
| Messages         | `/v1/messages`         | `anthropic`             |

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
