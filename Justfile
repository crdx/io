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
    unbuffer golangci-lint run --color never --fix | gostack

test:
    unbuffer go test -cover ./... | gostack --test

build:
    unbuffer go build -trimpath -o dist/oh ./cmd/oh | gostack

check:
    steps fmt vet lint1 lint2 mega test

oh *args:
    go run ./cmd/oh "$@"

[private]
mega:
    mega

[private]
vet:
    unbuffer go vet ./... | gostack

[private]
lint1:
    unbuffer golangci-lint run --color never | gostack

[private]
lint2:
    fd -tf -g '*.go' | xargs gopls check
