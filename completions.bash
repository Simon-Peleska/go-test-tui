#!/usr/bin/env bash
# Bash completions for go-test-tui.
# Sourced by the project's tools/completions.bash.

_go_test_tui() {
    local cur prev words cword
    _init_completion 2>/dev/null || {
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        words=("${COMP_WORDS[@]}")
        cword="$COMP_CWORD"
    }

    local subcommand=""
    local after_dashdash=0

    # Find the subcommand and whether we're after --
    for (( i=1; i<cword; i++ )); do
        case "${words[i]}" in
            --) after_dashdash=1; break ;;
            list|run|help) subcommand="${words[i]}" ;;
        esac
    done

    # After -- : complete go test flags
    if [[ "$after_dashdash" == 1 ]]; then
        local go_flags="-run -count -parallel -timeout -v -race -bench -benchtime -benchmem -coverprofile -failfast -short -vet"
        case "$prev" in
            -run)  _complete_test_names; return ;;
            -count|-parallel|-timeout|-bench|-benchtime|-coverprofile|-vet) return ;;
        esac
        COMPREPLY=($(compgen -W "$go_flags" -- "$cur"))
        return
    fi

    # run subcommand: same flags as top-level + --
    if [[ "$subcommand" == "run" ]]; then
        case "$prev" in
            -output-dir)
                COMPREPLY=($(compgen -d -- "$cur"))
                return
                ;;
        esac
        COMPREPLY=($(compgen -W "-output-dir -keep-logs -clean --" -- "$cur"))
        return
    fi

    # list subcommand
    if [[ "$subcommand" == "list" ]]; then
        case "$prev" in
            -status)
                COMPREPLY=($(compgen -W "failed pass skip" -- "$cur"))
                return
                ;;
            -output-dir)
                COMPREPLY=($(compgen -d -- "$cur"))
                return
                ;;
        esac
        # First positional arg to list: complete test names from last run
        local n_positional=0
        for (( i=2; i<cword; i++ )); do
            [[ "${words[i]}" != -* ]] && (( n_positional++ ))
        done
        if [[ "$n_positional" == 0 && "$cur" != -* ]]; then
            _complete_test_names
            return
        fi
        COMPREPLY=($(compgen -W "-status -output-dir" -- "$cur"))
        return
    fi

    # Top-level: offer subcommands if no subcommand yet and cur looks like a word
    if [[ "$subcommand" == "" && "$cur" != -* ]]; then
        COMPREPLY=($(compgen -W "run list help" -- "$cur"))
        [[ ${#COMPREPLY[@]} -gt 0 ]] && return
    fi

    # Top-level flags
    case "$prev" in
        -output-dir)
            COMPREPLY=($(compgen -d -- "$cur"))
            return
            ;;
    esac

    COMPREPLY=($(compgen -W "-output-dir -keep-logs -clean --" -- "$cur"))
}

# Complete test names from the last run's JSON log.
_complete_test_names() {
    local json
    for dir in ./test_logs/latest/test_output.json test_logs/latest/test_output.json; do
        [[ -f "$dir" ]] && json="$dir" && break
    done
    [[ -z "$json" ]] && return
    local names
    names=$(grep -o '"Test":"[^"]*"' "$json" 2>/dev/null | sed 's/"Test":"//;s/"//' | sort -u)
    COMPREPLY=($(compgen -W "$names" -- "$cur"))
}

complete -F _go_test_tui go-test-tui
