# Changelog

## [0.1.0] - 2026-08-17

### Added

Initial release.

Several primitive top-level packages:

- `agent`: conversation loop, with streaming, batching, and cancellation
- `tool`: tools, schemas, middleware, concurrency, and orchestration
- `toolbox`: implementation of read/ls/find/grep/write/edit/bash
- `session`: session saving and resumption, as an append-only journal
- `provider/codex`: the Responses API, the OpenAI way (for now)

Some tools:

- `cmd/login`: do the standard OAuth handshake and store the credentials
- `cmd/simulate`: serve a defined scenario as a simulation of the Responses API

A few examples:

- `cmd/weather`: define a tool, then ask a question that needs it
- `cmd/streaming`: print each event of a turn as it arrives
- `cmd/simple`: the same loop, with text fragments glued back into whole messages

A harness:

- `cmd/oh`: opinionated af
