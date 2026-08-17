set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

import? 'internal.just'

set positional-arguments

[private]
help:
    just --list --unsorted

fmt:
    go fmt ./...

fix:
    unbuffer golangci-lint run --color never --fix | gostack

test:
    unbuffer go test -cover ./... | gostack --test

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
