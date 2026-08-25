function _oh {
    local LINE WORD PREVIOUS KIND
    local -a TOKENS

    LINE=${COMP_LINE:0:COMP_POINT}
    read -ra TOKENS <<< "$LINE"

    if [[ -z $LINE || $LINE == *[[:space:]] ]]; then
        WORD=
        PREVIOUS=${TOKENS[-1]-}
    else
        WORD=${TOKENS[-1]-}
        PREVIOUS=${TOKENS[-2]-}
    fi

    case $PREVIOUS in
        -d | --workspace)
            mapfile -t COMPREPLY < <(compgen -d -- "$WORD")
            compopt -o filenames
            return
            ;;
        -r | --resume | --from) KIND=session ;;
        -c | --caps) KIND=caps ;;
        -t | --tool) KIND=tool ;;
        -m | --model)
            if [[ $WORD == *@* && $COMP_WORDBREAKS == *@* ]]; then
                KIND=effort
            else
                KIND=model
            fi
            ;;
        *)
            if [[ -n $WORD && $WORD != -* ]]; then
                return
            fi
            KIND=option
            ;;
    esac

    mapfile -t COMPREPLY < <("${COMP_WORDS[0]}" --complete "$KIND" "$WORD")

    if [[ $KIND == effort && ${#COMPREPLY[@]} -gt 0 ]]; then
        COMPREPLY=("${COMPREPLY[@]/#/@}")
    fi
}

complete -o default -F _oh oh
