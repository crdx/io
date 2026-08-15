set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

SESSION := "io-rig"
BIN     := "/tmp/io-rig.test"
WIDTH   := "100"
HEIGHT  := "24"

# build the visual pass: the harness's output layer over a fake model
build:
    go test -c -o {{ BIN }} ./cmd/harness/

# start the pane and run one turn against the fake model
up pass="TestVisual": build down
    tmux new-session -d -s {{ SESSION }} -x {{ WIDTH }} -y {{ HEIGHT }} \
        -e VISUAL=1 {{ BIN }} -test.run {{ pass }}
    tmux set-option -t {{ SESSION }} remain-on-exit on

# start the pane on the editor instead, which waits to be typed at
edit: (up "TestLine")

# rebuild and restart the app in the same pane
reload pass="TestVisual": build
    tmux respawn-pane -k -t {{ SESSION }} -e VISUAL=1 {{ BIN }} -test.run {{ pass }}

# settle and capture the pane -> tmp/<date>-<name>.txt
shot name: settle
    mkdir -p tmp
    tmux capture-pane -p -t {{ SESSION }} > tmp/$(date +%F)-{{ name }}.txt
    cat tmp/$(date +%F)-{{ name }}.txt

# as shot, but with the escape sequences left in, for checking a colour
sgr name: settle
    mkdir -p tmp
    tmux capture-pane -p -e -t {{ SESSION }} > tmp/$(date +%F)-{{ name }}.txt
    cat -v tmp/$(date +%F)-{{ name }}.txt

# as shot, numbered and with visible line ends, for spotting a stray blank line
lines name: settle
    mkdir -p tmp
    tmux capture-pane -p -t {{ SESSION }} > tmp/$(date +%F)-{{ name }}.txt
    cat -nA tmp/$(date +%F)-{{ name }}.txt

# type text into the pane, without pressing return
send text:
    tmux send-keys -t {{ SESSION }} -l '{{ text }}'

# press a key by name (Enter, Escape, C-c)
press key:
    tmux send-keys -t {{ SESSION }} '{{ key }}'

# send a raw escape sequence, for the keys tmux has no name for (e.g. '\e[1;5D' for ctrl+left)
raw sequence:
    tmux send-keys -t {{ SESSION }} -l "$(printf '{{ sequence }}')"

# wait for the app to finish redrawing before anything captures it
[private]
settle:
    sleep 0.4

# kill the rig
down:
    tmux kill-session -t {{ SESSION }} 2>/dev/null || true
