mod release

set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

import? 'local.just'

set positional-arguments

[private]
help:
    just --list --unsorted --list-submodules

fmt:
    go fmt ./...

fix:
    golangci-lint run --color never --fix

test:
    go test -cover ./...

build:
    go build -trimpath -o dist/oh ./cmd/oh

check:
    steps fmt vet lint1 lint2 mega test

oh *args:
    go run ./cmd/oh "$@"

[private]
mega:
    mega

[private]
vet:
    go vet ./...

[private]
lint1:
    golangci-lint run --color never

[private]
lint2:
    fd -tf -g '*.go' | xargs gopls check
