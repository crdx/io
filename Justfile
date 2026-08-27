mod release

set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

import? 'local.just'

set positional-arguments

LINT_CACHE := env('GOLANGCI_LINT_CACHE', home_directory() / '.cache' / 'golangci-lint')

export GOLANGCI_LINT_CACHE := LINT_CACHE / sha256(justfile_directory())

[private]
help:
    just --list --unsorted --list-submodules

dev:
    watchexec --debounce 500ms just check

fmt:
    go fmt ./...

fix:
    golangci-lint run --color never --fix

test:
    go test -cover ./...

# run a fuzzing campaign against one target, for a minute unless told otherwise
fuzz package target time='1m':
    go test ./{{ package }} -run '^$' -fuzz '^{{ target }}$' -fuzztime {{ time }}

# write what the tests drew back to the golden files
golden:
    go test ./cmd/oh ./cmd/oh/cli ./cmd/oh/commands -update

# what every package covers, least covered first
cov *args:
    #!/bin/bash
    set -euo pipefail
    PROFILE=$(mktemp -t io-cover.XXXXXX)
    trap 'rm -f "$PROFILE"' EXIT
    go test ./... -coverpkg=./... -coverprofile="$PROFILE" -count=1 > /dev/null
    if [[ $# -gt 0 ]]; then
        go tool cover -func="$PROFILE" | grep -E "$1" | grep -v " 100.0%$"
        exit
    fi
    ./script/coverage "$PROFILE"

# open the coverage of one package in a browser
covhtml package:
    #!/bin/bash
    set -euo pipefail
    PROFILE=$(mktemp -t io-cover.XXXXXX)
    trap 'rm -f "$PROFILE"' EXIT
    go test ./... -coverpkg=./{{ package }}/... -coverprofile="$PROFILE" -count=1 > /dev/null
    go tool cover -html="$PROFILE"

build:
    go build -trimpath -o dist/oh ./cmd/oh
    go build -trimpath -o dist/ohctl ./cmd/ohctl

check:
    steps fmt vet lint1 lint2 lint3 mega test

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

ohctl *args:
    go run ./cmd/ohctl "$@"

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
    OUTPUT="$(fd -tf -g '*.go' | xargs gopls check -severity=hint 2>&1)"
    STATUS=$?
    if [[ $STATUS -ne 0 || -n "$OUTPUT" ]]; then
        echo "$OUTPUT"
        exit 1
    fi

[private]
lint3:
    fd -tf -e go -X go run ./internal/lint/receivername
