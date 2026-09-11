# Changelog

## [N.N.N] - XXXX-XX-XX

- Tell agent that a finished job will notify
- Add max job wait time
- Show whole seconds for durations >= 1s
- Show when a limited usage window resets
- Detect valid Codex context windows the right way
- Allow bash within host network, toggled with `n` cap
- Rename `web_search` to `lookup`, toggled with `l` cap
- Rename `web_fetch` to `fetch`, toggled with `n` cap
- Remove the deprecated `s` cap
- Add flag to enable network in bash tool call
- Use the workspace read/write state to colour the `x` cap
- Let a file tool follow a symlinked path to the grant that owns it
- Add `[experimental]` config section
- Hold every configured path by its real path

## [0.5.0] - 2026-09-10

- Detect valid Codex models the right way
- Default to high effort level, and make it configurable
- Make fast mode default configurable
- Remove stray blank line from incoming mid-round notices
- Spell grant flags in capability order (`rxw`)
- Warn when [sandbox.read] is used redundantly
- Linkify paths in tool calls, reasoning, notices, everything!
- Prevent sub usage flashing on refresh

## [0.4.0] - 2026-09-10

- Preview sessions in session picker
- Name sandbox helper processes
- Restore the cache properly on resume
- Fix linkification of paths ending in dots

## [0.3.0] - 2026-09-10

- Add LLM simulator backend with `--demo`, or use the _Simulation_ option during onboarding
- Track input and output token API prices, stored when model list is updated
- Add session spend segment which shows the API cost of the current session
- Configure currency with `ui.currency` if you don't like US dollars
- Now `ohctl analyse` also reports models, spend, conversation, faults, and tool use
- Render markdown images inline
- Map /tmp paths within sandbox to host paths transparently
- Add timestamps and response decompression state to `wire.http` logs
- Make an unknown `config.toml` key be just a warning
- Read OpenAI subscription usage from account if stale on startup
- Fix some minor text wrapping, truncation, spacing, and hyperlink issues
- Stop tool call rows getting truncated an extra two chars
- Fix image rendering when several parallel reads are triggered
- Remove the extra blank line that was sometimes left behind on exit
- Shorten notices and format token counts consistently
- Remove the superfluous space before the image address

## [0.2.0] - 2026-09-09

### Added

#### Wire Protocols

- Anthropic Messages
- OpenAI Chat Completions
- OpenAI Responses

#### Providers

- Anthropic (Claude subscription)
- Codex (ChatGPT subscription)
- OpenCode Go
- Ollama (local, network)

#### Models

- Listing with `-l`
- Caching, refreshed when stale
- Legacy models filtered out
- Incompatible models filtered out
- Round-robin rotation
- Effort levels
- Fast mode

#### Tools

- `job`: run background commands
- `notify`: send desktop notifications
- `title`: name the session
- `web`: search and fetch pages
- `expose`: forward a port inwards
- Restrict with `-t`
- Configurable output cap
- Malformed call correction
- Out-of-band result viewing
- Batched concurrent calls
- Stale-file edits refused

#### Sandbox

- Namespaces, Landlock, Seccomp
- Virtual `/proc` and private `/tmp`
- Grants resolved per component
- Symlinks never followed
- File-level read, write, exec grants
- Process count limits
- File size limits
- Output size limits
- Processor time limits
- Port forwarding in both directions
- Session-scoped grants
- Grant management and revocation
- Offline Go module proxy
- Obviously, `--yolo` for the daring

#### Capabilities

- Read always granted
- Shell execution opt-in
- Workspace writes opt-in
- Repository history opt-in
- Web access opt-in
- Mid-session toggling with ctrl+x
- Recorded in the session, restored on resume

#### Interface

- Configurable input block segments:
    - Model
    - Fast Mode
    - Context
    - Usage
    - Cache Share
    - Turn Timer
    - Turn Count
    - Time
    - Git Branch
    - Session Name
    - Session Emoji
    - Workspace Directory
    - Path Grants
    - Exposed Ports
    - Jobs
    - Mode Toggle
    - Activity Spinner
    - Scroll Overflow
- Streaming modes:
    - ASAP
    - Line
    - Paced
- Reasoning display:
    - Plain
    - Markdown
- Configurable output grouping
- Incremental Markdown rendering
- Mermaid diagrams from fenced blocks
- Syntax highlighting
- OSC 8 links
- Message copying over OSC 52
- Pictures drawn with kitty graphics
- Terminal title management

#### TUIs

- Model picker
- Session picker
- Onboarding wizard

#### Input

- Multi-line editing
- Scrollable input
- History recall ("reverse-i-search")
- Pasted text handled sanely
- Pasted images handled

#### Commands

- `/conf`: edit the configuration
- `/copy`: copy a target
- `/edit`: edit a target
- `/info`: show the session
- `/open`: open a target
- `/new`: start a session
- `/expose`: expose a host port
- `/grant`: grant path access
- `/grants`: list grants and routes
- `/revoke`: revoke a grant or route
- `/jobs`: list background jobs
- `/job`: manage a background job
- `/help`: list the commands
- `/fork`: fork the session
- `//double-slash` snippets
- Unknown commands rejected
- Tab completion

#### Agent Turns

- Message queueing
- Mid-turn interjection
- Pokes on an unanswered turn
- Idle-timeout monitoring
- Retries with backoff
- Retriable HTTP 507
- Usage-limit detection
- Automatic recovery probing
- Cache-loss reporting
- Cache-rebuild reporting
- Prompt-prefix violation reporting
- Notifications on turn finish
- Notifications on session death

#### Sessions

- Versioned journal
- Migrations, locking
- Corrupted journals refused
- Frozen:
    - Tools
    - Model
    - Workspace
    - Confinement
    - Capabilities
- Archiving
- Restoring
- Deletion
- Forking
- Source chat drops
- Compact Markdown transcripts
- HTTP wire logs
- Transcript regeneration

#### Configuration

- Fully XDG compliant
- Local `oh.toml` overrides (lists merged)
- Paths resolved per file
- Ordered settings replaced
- Local settings named at startup
- Defaults with `caps.default`
- Live reloading
- Skills, skill include and exclude patterns

#### Command Line

- Session resumption with `-r`
- Model selection with `-m`
- Capability selection with `-c`
- Print mode with `-p`
- Initial files with `--add`
- Piped stdin joined with arguments
- OAuth login with `-L`
- Sub usage reporting

#### Maintenance

- `ohctl sessions`
- `ohctl analyse`
- `ohctl regenerate`
- `ohctl migrate`
- `ohctl gc`

#### Development

- Replay-based test suite
- Terminal goldens

#### Linters

- `abbreviation`
- `adjective`
- `boolname`
- `receivername`
- `stdstream`

## [0.1.0] - 2026-08-17

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
