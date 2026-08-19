set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

SESSION   := "io-rig"
BIN       := "/tmp/io-rig.test"
PICKER    := "/tmp/io-rig-picker.test"
OH        := "/tmp/io-rig.oh"
SIM       := "/tmp/io-rig.sim"
SCENARIO  := "internal/sim/scenarios/success.toml"
WORKSPACE := "tmp/workspace"
ADDRESS   := "localhost:8117"
WIDTH     := "100"
HEIGHT    := "24"

# build the visual pass: the harness's output layer over a fake model
build:
    go test -c -o {{ BIN }} ./cmd/oh/

# start the pane and run one turn against the fake model
up pass="TestVisual": build down
    tmux new-session -d -s {{ SESSION }} -x {{ WIDTH }} -y {{ HEIGHT }} \
        -e RIG=1 {{ BIN }} -test.run {{ pass }}
    tmux set-option -t {{ SESSION }} remain-on-exit on

# start the pane on the session picker instead
pick: down
    go test -c -o {{ PICKER }} ./cmd/ohs/
    tmux new-session -d -s {{ SESSION }} -x {{ WIDTH }} -y {{ HEIGHT }} \
        -e RIG=1 {{ PICKER }} -test.run TestPick
    tmux set-option -t {{ SESSION }} remain-on-exit on

# build the harness and the stand-in endpoint
harness:
    go build -o {{ OH }} ./cmd/oh/
    go build -o {{ SIM }} ./cmd/simulate/

# run the harness for real against a scenario, in a workspace of its own
live scenario=SCENARIO workspace=WORKSPACE opening="" caps="rwx": harness down
    mkdir -p {{ workspace }}
    tmux new-session -d -s {{ SESSION }} -x {{ WIDTH }} -y {{ HEIGHT }} \
        {{ SIM }} --scenario {{ scenario }} --address {{ ADDRESS }}
    tmux set-option -t {{ SESSION }} remain-on-exit on
    sleep 0.3
    tmux split-window -t {{ SESSION }} -v -e OH_ENDPOINT_URL=http://{{ ADDRESS }} \
        {{ OH }} --workspace {{ workspace }} --caps {{ caps }} {{ opening }}
    tmux select-pane -t {{ SESSION }}.{bottom}

# type into the harness pane and press return
say text:
    tmux send-keys -t {{ SESSION }}.{bottom} -l '{{ text }}'
    tmux send-keys -t {{ SESSION }}.{bottom} Enter

# rebuild and restart the app in the same pane
reload pass="TestVisual": build
    tmux respawn-pane -k -t {{ SESSION }} -e RIG=1 {{ BIN }} -test.run {{ pass }}

# resize the rig terminal to trigger a full conversation repaint
resize width=WIDTH height=HEIGHT:
    tmux resize-window -t {{ SESSION }} -x {{ width }} -y {{ height }}

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
