function _ohctl {
    local LINE WORD COMMAND KIND
    local -a TOKENS

    LINE=${COMP_LINE:0:COMP_POINT}
    read -ra TOKENS <<< "$LINE"

    if [[ -z $LINE || $LINE == *[[:space:]] ]]; then
        WORD=
    else
        WORD=${TOKENS[-1]-}
    fi

    COMMAND=${TOKENS[1]-}

    if [[ -z $COMMAND || ( ${#TOKENS[@]} -eq 2 && -n $WORD ) ]]; then
        KIND=command
    elif [[ $COMMAND == analyse || $COMMAND == regen || $COMMAND == migrate ]]; then
        KIND=session
    else
        return
    fi

    mapfile -t COMPREPLY < <("${COMP_WORDS[0]}" --complete "$KIND" "$WORD")
}

complete -o default -F _ohctl ohctl
