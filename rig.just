set quiet := true
set shell := ["bash", "-cu", "-o", "pipefail"]

SESSION   := "io-rig"
BIN       := "/tmp/io-rig.test"
PICKER    := "/tmp/io-rig-picker.test"
OH        := "/tmp/io-rig.oh"
SIM       := "/tmp/io-rig.sim"
SCENARIO  := "internal/sim/scenarios/success.toml"
WORKSPACE := "tmp/workspace"
STATE     := justfile_directory() / "tmp/rig-state"
SESSIONS  := STATE / "org.crdx/oh/sessions"
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

# start an isolated session whose read snapshot can be resumed
resume-up scenario="internal/sim/scenarios/resume-read.toml": harness down
    rm -rf {{ STATE }} {{ WORKSPACE }}
    mkdir -p {{ STATE }} {{ WORKSPACE }}
    printf 'one\n' > {{ WORKSPACE }}/target.txt
    tmux new-session -d -s {{ SESSION }} -x {{ WIDTH }} -y {{ HEIGHT }} \
        {{ SIM }} --scenario {{ scenario }} --address {{ ADDRESS }}
    tmux set-option -t {{ SESSION }} remain-on-exit on
    sleep 0.3
    tmux split-window -t {{ SESSION }} -v \
        -e OH_ENDPOINT_URL=http://{{ ADDRESS }} -e XDG_STATE_HOME={{ STATE }} \
        {{ OH }} --workspace {{ WORKSPACE }} --caps rw
    tmux select-pane -t {{ SESSION }}.{bottom}

# restart the fake endpoint and resume the isolated session
resume scenario="internal/sim/scenarios/resume-edit.toml": harness
    #!/bin/bash
    set -euo pipefail
    ID="$(find {{ SESSIONS }} -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | head -n 1)"
    test -n "$ID"
    tmux respawn-pane -k -t {{ SESSION }}.{top} \
        {{ SIM }} --scenario {{ scenario }} --address {{ ADDRESS }}
    sleep 0.3
    tmux respawn-pane -k -t {{ SESSION }}.{bottom} \
        -e OH_ENDPOINT_URL=http://{{ ADDRESS }} -e XDG_STATE_HOME={{ STATE }} \
        {{ OH }} --caps rw --resume "$ID"
    tmux select-pane -t {{ SESSION }}.{bottom}

# stop the harness while leaving its fake endpoint available
resume-stop:
    tmux respawn-pane -k -t {{ SESSION }}.{bottom} /bin/true

# replace the resume target while the harness is stopped
resume-target content:
    printf '%s\n' '{{ content }}' > {{ WORKSPACE }}/target.txt

# print the isolated session journal
resume-journal:
    #!/bin/bash
    set -euo pipefail
    ID="$(find {{ SESSIONS }} -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | head -n 1)"
    test -n "$ID"
    cat {{ SESSIONS }}/$ID/session.jsonl

# show the current resume target contents
resume-target-show:
    cat {{ WORKSPACE }}/target.txt

# attach to the interactive rig session
attach:
    tmux attach-session -t {{ SESSION }}

# print process status and scrollback for both resume panes
resume-status:
    #!/bin/bash
    set -euo pipefail
    FORMAT='#{pane_index} #{pane_dead} #{pane_dead_status} #{pane_current_command}'
    tmux list-panes -t {{ SESSION }} -F "$FORMAT"
    for POSITION in top bottom; do
        echo "--- $POSITION ---"
        tmux capture-pane -p -S - -t {{ SESSION }}.$POSITION
    done

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
