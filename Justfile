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
    go build -trimpath -o dist/ohs ./cmd/ohs

check:
    steps fmt vet lint1 lint2 mega test

# download the API references for each provider
refs:
    #!/bin/bash
    set -euo pipefail
    GREEN='\e[32m'
    NC='\e[0m'
    for SOURCES in provider/*/reference/sources.txt; do
        DIRECTORY="$(dirname "$SOURCES")"
        while read -r NAME ADDRESS; do
            if [[ -z "$NAME" || "$NAME" == \#* ]]; then
                continue
            fi
            curl -fsSL --retry 2 -o "$DIRECTORY/$NAME" "$ADDRESS"
            echo -e "${GREEN}${DIRECTORY}/${NAME}${NC} $(wc -c < "$DIRECTORY/$NAME")B"
        done < "$SOURCES"
        mega -x "$DIRECTORY"
    done

oh *args:
    go run ./cmd/oh "$@"

ohm *args:
    go run ./cmd/oh -crx "$@"

ohs:
    go run ./cmd/ohs

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
    #!/bin/bash
    set -euo pipefail
    OUTPUT="$(fd -tf -g '*.go' | xargs gopls check 2>&1)"
    STATUS=$?
    if [[ $STATUS -ne 0 || -n "$OUTPUT" ]]; then
        echo "$OUTPUT"
        exit 1
    fi
