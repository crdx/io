# io

**io** is an agent harness primitive and world builder written in Go.

## Installation

```sh
go get crdx.org/io
```

## Features

- TBD

## Non-Features

- Handoff between providers
- Context compaction

## Usage

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

answer, _ := assistant.Send("what is the weather in London?")
fmt.Println(answer) // => "It's raining in London."
```

`Send` triggers a model turn until completion.

## Built-in tools (coming soon)

See `tools/` for the implementations of the built-in tools.

| Tool    | Arguments                      | Does                             | Type  |
|---------|--------------------------------|----------------------------------|-------|
| `read`  | `path`, `offset`, `limit`      | Read a file                      | Read  |
| `ls`    | `path`                         | List a directory                 | Read  |
| `find`  | `pattern`, `path`              | Find files                       | Read  |
| `grep`  | `pattern`, `path`, `glob`      | Search file contents             | Read  |
| `write` | `path`, `content`              | Write a whole file               | Write |
| `edit`  | `path`, `old_text`, `new_text` | Find and replace an exact string | Write |

Read-only calls run concurrently, at most ten at a time.

All path operations are wrapped in an `os.Root`, and writing to `.git` can be optionally denied.

## Contributions

Open an [issue](https://github.com/crdx/io/issues) or send a [pull request](https://github.com/crdx/io/pulls).

## Licence

[GPLv3](LICENCE).
