# bash completion for platformctl                          -*- shell-script -*-

__platformctl_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE:-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}

# Homebrew on Macs have version 1.3 of bash-completion which doesn't include
# _init_completion. This is a very minimal version of that function.
__platformctl_init_completion()
{
    COMPREPLY=()
    _get_comp_words_by_ref "$@" cur prev words cword
}

__platformctl_index_of_word()
{
    local w word=$1
    shift
    index=0
    for w in "$@"; do
        [[ $w = "$word" ]] && return
        index=$((index+1))
    done
    index=-1
}

__platformctl_contains_word()
{
    local w word=$1; shift
    for w in "$@"; do
        [[ $w = "$word" ]] && return
    done
    return 1
}

__platformctl_handle_go_custom_completion()
{
    __platformctl_debug "${FUNCNAME[0]}: cur is ${cur}, words[*] is ${words[*]}, #words[@] is ${#words[@]}"

    local shellCompDirectiveError=1
    local shellCompDirectiveNoSpace=2
    local shellCompDirectiveNoFileComp=4
    local shellCompDirectiveFilterFileExt=8
    local shellCompDirectiveFilterDirs=16

    local out requestComp lastParam lastChar comp directive args

    # Prepare the command to request completions for the program.
    # Calling ${words[0]} instead of directly platformctl allows handling aliases
    args=("${words[@]:1}")
    # Disable ActiveHelp which is not supported for bash completion v1
    requestComp="PLATFORMCTL_ACTIVE_HELP=0 ${words[0]} __completeNoDesc ${args[*]}"

    lastParam=${words[$((${#words[@]}-1))]}
    lastChar=${lastParam:$((${#lastParam}-1)):1}
    __platformctl_debug "${FUNCNAME[0]}: lastParam ${lastParam}, lastChar ${lastChar}"

    if [ -z "${cur}" ] && [ "${lastChar}" != "=" ]; then
        # If the last parameter is complete (there is a space following it)
        # We add an extra empty parameter so we can indicate this to the go method.
        __platformctl_debug "${FUNCNAME[0]}: Adding extra empty parameter"
        requestComp="${requestComp} \"\""
    fi

    __platformctl_debug "${FUNCNAME[0]}: calling ${requestComp}"
    # Use eval to handle any environment variables and such
    out=$(eval "${requestComp}" 2>/dev/null)

    # Extract the directive integer at the very end of the output following a colon (:)
    directive=${out##*:}
    # Remove the directive
    out=${out%:*}
    if [ "${directive}" = "${out}" ]; then
        # There is not directive specified
        directive=0
    fi
    __platformctl_debug "${FUNCNAME[0]}: the completion directive is: ${directive}"
    __platformctl_debug "${FUNCNAME[0]}: the completions are: ${out}"

    if [ $((directive & shellCompDirectiveError)) -ne 0 ]; then
        # Error code.  No completion.
        __platformctl_debug "${FUNCNAME[0]}: received error from custom completion go code"
        return
    else
        if [ $((directive & shellCompDirectiveNoSpace)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __platformctl_debug "${FUNCNAME[0]}: activating no space"
                compopt -o nospace
            fi
        fi
        if [ $((directive & shellCompDirectiveNoFileComp)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __platformctl_debug "${FUNCNAME[0]}: activating no file completion"
                compopt +o default
            fi
        fi
    fi

    if [ $((directive & shellCompDirectiveFilterFileExt)) -ne 0 ]; then
        # File extension filtering
        local fullFilter filter filteringCmd
        # Do not use quotes around the $out variable or else newline
        # characters will be kept.
        for filter in ${out}; do
            fullFilter+="$filter|"
        done

        filteringCmd="_filedir $fullFilter"
        __platformctl_debug "File filtering command: $filteringCmd"
        $filteringCmd
    elif [ $((directive & shellCompDirectiveFilterDirs)) -ne 0 ]; then
        # File completion for directories only
        local subdir
        # Use printf to strip any trailing newline
        subdir=$(printf "%s" "${out}")
        if [ -n "$subdir" ]; then
            __platformctl_debug "Listing directories in $subdir"
            __platformctl_handle_subdirs_in_dir_flag "$subdir"
        else
            __platformctl_debug "Listing directories in ."
            _filedir -d
        fi
    else
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${out}" -- "$cur")
    fi
}

__platformctl_handle_reply()
{
    __platformctl_debug "${FUNCNAME[0]}"
    local comp
    case $cur in
        -*)
            if [[ $(type -t compopt) = "builtin" ]]; then
                compopt -o nospace
            fi
            local allflags
            if [ ${#must_have_one_flag[@]} -ne 0 ]; then
                allflags=("${must_have_one_flag[@]}")
            else
                allflags=("${flags[*]} ${two_word_flags[*]}")
            fi
            while IFS='' read -r comp; do
                COMPREPLY+=("$comp")
            done < <(compgen -W "${allflags[*]}" -- "$cur")
            if [[ $(type -t compopt) = "builtin" ]]; then
                [[ "${COMPREPLY[0]}" == *= ]] || compopt +o nospace
            fi

            # complete after --flag=abc
            if [[ $cur == *=* ]]; then
                if [[ $(type -t compopt) = "builtin" ]]; then
                    compopt +o nospace
                fi

                local index flag
                flag="${cur%=*}"
                __platformctl_index_of_word "${flag}" "${flags_with_completion[@]}"
                COMPREPLY=()
                if [[ ${index} -ge 0 ]]; then
                    PREFIX=""
                    cur="${cur#*=}"
                    ${flags_completion[${index}]}
                    if [ -n "${ZSH_VERSION:-}" ]; then
                        # zsh completion needs --flag= prefix
                        eval "COMPREPLY=( \"\${COMPREPLY[@]/#/${flag}=}\" )"
                    fi
                fi
            fi

            if [[ -z "${flag_parsing_disabled}" ]]; then
                # If flag parsing is enabled, we have completed the flags and can return.
                # If flag parsing is disabled, we may not know all (or any) of the flags, so we fallthrough
                # to possibly call handle_go_custom_completion.
                return 0;
            fi
            ;;
    esac

    # check if we are handling a flag with special work handling
    local index
    __platformctl_index_of_word "${prev}" "${flags_with_completion[@]}"
    if [[ ${index} -ge 0 ]]; then
        ${flags_completion[${index}]}
        return
    fi

    # we are parsing a flag and don't have a special handler, no completion
    if [[ ${cur} != "${words[cword]}" ]]; then
        return
    fi

    local completions
    completions=("${commands[@]}")
    if [[ ${#must_have_one_noun[@]} -ne 0 ]]; then
        completions+=("${must_have_one_noun[@]}")
    elif [[ -n "${has_completion_function}" ]]; then
        # if a go completion function is provided, defer to that function
        __platformctl_handle_go_custom_completion
    fi
    if [[ ${#must_have_one_flag[@]} -ne 0 ]]; then
        completions+=("${must_have_one_flag[@]}")
    fi
    while IFS='' read -r comp; do
        COMPREPLY+=("$comp")
    done < <(compgen -W "${completions[*]}" -- "$cur")

    if [[ ${#COMPREPLY[@]} -eq 0 && ${#noun_aliases[@]} -gt 0 && ${#must_have_one_noun[@]} -ne 0 ]]; then
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${noun_aliases[*]}" -- "$cur")
    fi

    if [[ ${#COMPREPLY[@]} -eq 0 ]]; then
        if declare -F __platformctl_custom_func >/dev/null; then
            # try command name qualified custom func
            __platformctl_custom_func
        else
            # otherwise fall back to unqualified for compatibility
            declare -F __custom_func >/dev/null && __custom_func
        fi
    fi

    # available in bash-completion >= 2, not always present on macOS
    if declare -F __ltrim_colon_completions >/dev/null; then
        __ltrim_colon_completions "$cur"
    fi

    # If there is only 1 completion and it is a flag with an = it will be completed
    # but we don't want a space after the =
    if [[ "${#COMPREPLY[@]}" -eq "1" ]] && [[ $(type -t compopt) = "builtin" ]] && [[ "${COMPREPLY[0]}" == --*= ]]; then
       compopt -o nospace
    fi
}

# The arguments should be in the form "ext1|ext2|extn"
__platformctl_handle_filename_extension_flag()
{
    local ext="$1"
    _filedir "@(${ext})"
}

__platformctl_handle_subdirs_in_dir_flag()
{
    local dir="$1"
    pushd "${dir}" >/dev/null 2>&1 && _filedir -d && popd >/dev/null 2>&1 || return
}

__platformctl_handle_flag()
{
    __platformctl_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    # if a command required a flag, and we found it, unset must_have_one_flag()
    local flagname=${words[c]}
    local flagvalue=""
    # if the word contained an =
    if [[ ${words[c]} == *"="* ]]; then
        flagvalue=${flagname#*=} # take in as flagvalue after the =
        flagname=${flagname%=*} # strip everything after the =
        flagname="${flagname}=" # but put the = back
    fi
    __platformctl_debug "${FUNCNAME[0]}: looking for ${flagname}"
    if __platformctl_contains_word "${flagname}" "${must_have_one_flag[@]}"; then
        must_have_one_flag=()
    fi

    # if you set a flag which only applies to this command, don't show subcommands
    if __platformctl_contains_word "${flagname}" "${local_nonpersistent_flags[@]}"; then
      commands=()
    fi

    # keep flag value with flagname as flaghash
    # flaghash variable is an associative array which is only supported in bash > 3.
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        if [ -n "${flagvalue}" ] ; then
            flaghash[${flagname}]=${flagvalue}
        elif [ -n "${words[ $((c+1)) ]}" ] ; then
            flaghash[${flagname}]=${words[ $((c+1)) ]}
        else
            flaghash[${flagname}]="true" # pad "true" for bool flag
        fi
    fi

    # skip the argument to a two word flag
    if [[ ${words[c]} != *"="* ]] && __platformctl_contains_word "${words[c]}" "${two_word_flags[@]}"; then
        __platformctl_debug "${FUNCNAME[0]}: found a flag ${words[c]}, skip the next argument"
        c=$((c+1))
        # if we are looking for a flags value, don't show commands
        if [[ $c -eq $cword ]]; then
            commands=()
        fi
    fi

    c=$((c+1))

}

__platformctl_handle_noun()
{
    __platformctl_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    if __platformctl_contains_word "${words[c]}" "${must_have_one_noun[@]}"; then
        must_have_one_noun=()
    elif __platformctl_contains_word "${words[c]}" "${noun_aliases[@]}"; then
        must_have_one_noun=()
    fi

    nouns+=("${words[c]}")
    c=$((c+1))
}

__platformctl_handle_command()
{
    __platformctl_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    local next_command
    if [[ -n ${last_command} ]]; then
        next_command="_${last_command}_${words[c]//:/__}"
    else
        if [[ $c -eq 0 ]]; then
            next_command="_platformctl_root_command"
        else
            next_command="_${words[c]//:/__}"
        fi
    fi
    c=$((c+1))
    __platformctl_debug "${FUNCNAME[0]}: looking for ${next_command}"
    declare -F "$next_command" >/dev/null && $next_command
}

__platformctl_handle_word()
{
    if [[ $c -ge $cword ]]; then
        __platformctl_handle_reply
        return
    fi
    __platformctl_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"
    if [[ "${words[c]}" == -* ]]; then
        __platformctl_handle_flag
    elif __platformctl_contains_word "${words[c]}" "${commands[@]}"; then
        __platformctl_handle_command
    elif [[ $c -eq 0 ]]; then
        __platformctl_handle_command
    elif __platformctl_contains_word "${words[c]}" "${command_aliases[@]}"; then
        # aliashash variable is an associative array which is only supported in bash > 3.
        if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
            words[c]=${aliashash[${words[c]}]}
            __platformctl_handle_command
        else
            __platformctl_handle_noun
        fi
    else
        __platformctl_handle_noun
    fi
    __platformctl_handle_word
}

_platformctl_app_abort()
{
    last_command="platformctl_app_abort"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--confirm=")
    two_word_flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm=")
    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--reason=")
    two_word_flags+=("--reason")
    local_nonpersistent_flags+=("--reason")
    local_nonpersistent_flags+=("--reason=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_flag+=("--confirm=")
    must_have_one_flag+=("--reason=")
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_create()
{
    last_command="platformctl_app_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--database=")
    two_word_flags+=("--database")
    local_nonpersistent_flags+=("--database")
    local_nonpersistent_flags+=("--database=")
    flags+=("--image-repository=")
    two_word_flags+=("--image-repository")
    local_nonpersistent_flags+=("--image-repository")
    local_nonpersistent_flags+=("--image-repository=")
    flags+=("--image-tag=")
    two_word_flags+=("--image-tag")
    local_nonpersistent_flags+=("--image-tag")
    local_nonpersistent_flags+=("--image-tag=")
    flags+=("--max-replicas=")
    two_word_flags+=("--max-replicas")
    local_nonpersistent_flags+=("--max-replicas")
    local_nonpersistent_flags+=("--max-replicas=")
    flags+=("--min-replicas=")
    two_word_flags+=("--min-replicas")
    local_nonpersistent_flags+=("--min-replicas")
    local_nonpersistent_flags+=("--min-replicas=")
    flags+=("--owner=")
    two_word_flags+=("--owner")
    local_nonpersistent_flags+=("--owner")
    local_nonpersistent_flags+=("--owner=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--port=")
    two_word_flags+=("--port")
    local_nonpersistent_flags+=("--port")
    local_nonpersistent_flags+=("--port=")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_delete()
{
    last_command="platformctl_app_delete"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--acknowledge-data-loss")
    local_nonpersistent_flags+=("--acknowledge-data-loss")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_doctor()
{
    last_command="platformctl_app_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_finalize()
{
    last_command="platformctl_app_finalize"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--approval-revision=")
    two_word_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision=")
    flags+=("--deletion-request=")
    two_word_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_list()
{
    last_command="platformctl_app_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_logs()
{
    last_command="platformctl_app_logs"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--follow")
    flags+=("-f")
    local_nonpersistent_flags+=("--follow")
    local_nonpersistent_flags+=("-f")
    flags+=("--historical")
    local_nonpersistent_flags+=("--historical")
    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--since=")
    two_word_flags+=("--since")
    local_nonpersistent_flags+=("--since")
    local_nonpersistent_flags+=("--since=")
    flags+=("--tail=")
    two_word_flags+=("--tail")
    local_nonpersistent_flags+=("--tail")
    local_nonpersistent_flags+=("--tail=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_policy()
{
    last_command="platformctl_app_policy"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_promote()
{
    last_command="platformctl_app_promote"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--confirm=")
    two_word_flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm=")
    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--reason=")
    two_word_flags+=("--reason")
    local_nonpersistent_flags+=("--reason")
    local_nonpersistent_flags+=("--reason=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_flag+=("--confirm=")
    must_have_one_flag+=("--reason=")
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_provenance()
{
    last_command="platformctl_app_provenance"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_rollout()
{
    last_command="platformctl_app_rollout"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_slo()
{
    last_command="platformctl_app_slo"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_status()
{
    last_command="platformctl_app_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_traces()
{
    last_command="platformctl_app_traces"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--limit=")
    two_word_flags+=("--limit")
    local_nonpersistent_flags+=("--limit")
    local_nonpersistent_flags+=("--limit=")
    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--trace-id=")
    two_word_flags+=("--trace-id")
    local_nonpersistent_flags+=("--trace-id")
    local_nonpersistent_flags+=("--trace-id=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app_update()
{
    last_command="platformctl_app_update"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--database=")
    two_word_flags+=("--database")
    local_nonpersistent_flags+=("--database")
    local_nonpersistent_flags+=("--database=")
    flags+=("--image-repository=")
    two_word_flags+=("--image-repository")
    local_nonpersistent_flags+=("--image-repository")
    local_nonpersistent_flags+=("--image-repository=")
    flags+=("--image-tag=")
    two_word_flags+=("--image-tag")
    local_nonpersistent_flags+=("--image-tag")
    local_nonpersistent_flags+=("--image-tag=")
    flags+=("--max-replicas=")
    two_word_flags+=("--max-replicas")
    local_nonpersistent_flags+=("--max-replicas")
    local_nonpersistent_flags+=("--max-replicas=")
    flags+=("--min-replicas=")
    two_word_flags+=("--min-replicas")
    local_nonpersistent_flags+=("--min-replicas")
    local_nonpersistent_flags+=("--min-replicas=")
    flags+=("--owner=")
    two_word_flags+=("--owner")
    local_nonpersistent_flags+=("--owner")
    local_nonpersistent_flags+=("--owner=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--port=")
    two_word_flags+=("--port")
    local_nonpersistent_flags+=("--port")
    local_nonpersistent_flags+=("--port=")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_app()
{
    last_command="platformctl_app"

    command_aliases=()

    commands=()
    commands+=("abort")
    commands+=("create")
    commands+=("delete")
    commands+=("doctor")
    commands+=("finalize")
    commands+=("list")
    commands+=("logs")
    commands+=("policy")
    commands+=("promote")
    commands+=("provenance")
    commands+=("rollout")
    commands+=("slo")
    commands+=("status")
    commands+=("traces")
    commands+=("update")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_cluster_down()
{
    last_command="platformctl_cluster_down"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_cluster_status()
{
    last_command="platformctl_cluster_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_cluster_up()
{
    last_command="platformctl_cluster_up"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_cluster()
{
    last_command="platformctl_cluster"

    command_aliases=()

    commands=()
    commands+=("down")
    commands+=("status")
    commands+=("up")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_completion()
{
    last_command="platformctl_completion"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    must_have_one_noun+=("bash")
    must_have_one_noun+=("fish")
    must_have_one_noun+=("powershell")
    must_have_one_noun+=("zsh")
    noun_aliases=()
}

_platformctl_config_init()
{
    last_command="platformctl_config_init"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--branch=")
    two_word_flags+=("--branch")
    local_nonpersistent_flags+=("--branch")
    local_nonpersistent_flags+=("--branch=")
    flags+=("--checkout=")
    two_word_flags+=("--checkout")
    local_nonpersistent_flags+=("--checkout")
    local_nonpersistent_flags+=("--checkout=")
    flags+=("--cluster=")
    two_word_flags+=("--cluster")
    local_nonpersistent_flags+=("--cluster")
    local_nonpersistent_flags+=("--cluster=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--http-port=")
    two_word_flags+=("--http-port")
    local_nonpersistent_flags+=("--http-port")
    local_nonpersistent_flags+=("--http-port=")
    flags+=("--https-port=")
    two_word_flags+=("--https-port")
    local_nonpersistent_flags+=("--https-port")
    local_nonpersistent_flags+=("--https-port=")
    flags+=("--kube-context=")
    two_word_flags+=("--kube-context")
    local_nonpersistent_flags+=("--kube-context")
    local_nonpersistent_flags+=("--kube-context=")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--repository=")
    two_word_flags+=("--repository")
    local_nonpersistent_flags+=("--repository")
    local_nonpersistent_flags+=("--repository=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_config_view()
{
    last_command="platformctl_config_view"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_config()
{
    last_command="platformctl_config"

    command_aliases=()

    commands=()
    commands+=("init")
    commands+=("view")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_context_delete()
{
    last_command="platformctl_context_delete"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_context_list()
{
    last_command="platformctl_context_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_context_set()
{
    last_command="platformctl_context_set"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--branch=")
    two_word_flags+=("--branch")
    local_nonpersistent_flags+=("--branch")
    local_nonpersistent_flags+=("--branch=")
    flags+=("--checkout=")
    two_word_flags+=("--checkout")
    local_nonpersistent_flags+=("--checkout")
    local_nonpersistent_flags+=("--checkout=")
    flags+=("--cluster=")
    two_word_flags+=("--cluster")
    local_nonpersistent_flags+=("--cluster")
    local_nonpersistent_flags+=("--cluster=")
    flags+=("--http-port=")
    two_word_flags+=("--http-port")
    local_nonpersistent_flags+=("--http-port")
    local_nonpersistent_flags+=("--http-port=")
    flags+=("--https-port=")
    two_word_flags+=("--https-port")
    local_nonpersistent_flags+=("--https-port")
    local_nonpersistent_flags+=("--https-port=")
    flags+=("--kube-context=")
    two_word_flags+=("--kube-context")
    local_nonpersistent_flags+=("--kube-context")
    local_nonpersistent_flags+=("--kube-context=")
    flags+=("--kubeconfig=")
    two_word_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--repository=")
    two_word_flags+=("--repository")
    local_nonpersistent_flags+=("--repository")
    local_nonpersistent_flags+=("--repository=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_context_use()
{
    last_command="platformctl_context_use"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_context()
{
    last_command="platformctl_context"

    command_aliases=()

    commands=()
    commands+=("delete")
    commands+=("list")
    commands+=("set")
    commands+=("use")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database_backups()
{
    last_command="platformctl_database_backups"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database_create()
{
    last_command="platformctl_database_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backup-retention=")
    two_word_flags+=("--backup-retention")
    local_nonpersistent_flags+=("--backup-retention")
    local_nonpersistent_flags+=("--backup-retention=")
    flags+=("--backup-schedule=")
    two_word_flags+=("--backup-schedule")
    local_nonpersistent_flags+=("--backup-schedule")
    local_nonpersistent_flags+=("--backup-schedule=")
    flags+=("--instances=")
    two_word_flags+=("--instances")
    local_nonpersistent_flags+=("--instances")
    local_nonpersistent_flags+=("--instances=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--storage=")
    two_word_flags+=("--storage")
    local_nonpersistent_flags+=("--storage")
    local_nonpersistent_flags+=("--storage=")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database_delete()
{
    last_command="platformctl_database_delete"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--acknowledge-data-loss")
    local_nonpersistent_flags+=("--acknowledge-data-loss")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database_finalize()
{
    last_command="platformctl_database_finalize"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--approval-revision=")
    two_word_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision=")
    flags+=("--deletion-request=")
    two_word_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database_restore()
{
    last_command="platformctl_database_restore"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backup-retention=")
    two_word_flags+=("--backup-retention")
    local_nonpersistent_flags+=("--backup-retention")
    local_nonpersistent_flags+=("--backup-retention=")
    flags+=("--backup-schedule=")
    two_word_flags+=("--backup-schedule")
    local_nonpersistent_flags+=("--backup-schedule")
    local_nonpersistent_flags+=("--backup-schedule=")
    flags+=("--instances=")
    two_word_flags+=("--instances")
    local_nonpersistent_flags+=("--instances")
    local_nonpersistent_flags+=("--instances=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--source-server-name=")
    two_word_flags+=("--source-server-name")
    local_nonpersistent_flags+=("--source-server-name")
    local_nonpersistent_flags+=("--source-server-name=")
    flags+=("--storage=")
    two_word_flags+=("--storage")
    local_nonpersistent_flags+=("--storage")
    local_nonpersistent_flags+=("--storage=")
    flags+=("--target-time=")
    two_word_flags+=("--target-time")
    local_nonpersistent_flags+=("--target-time")
    local_nonpersistent_flags+=("--target-time=")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database_status()
{
    last_command="platformctl_database_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--namespace=")
    two_word_flags+=("--namespace")
    two_word_flags+=("-n")
    local_nonpersistent_flags+=("--namespace")
    local_nonpersistent_flags+=("--namespace=")
    local_nonpersistent_flags+=("-n")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database_update()
{
    last_command="platformctl_database_update"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--backup-retention=")
    two_word_flags+=("--backup-retention")
    local_nonpersistent_flags+=("--backup-retention")
    local_nonpersistent_flags+=("--backup-retention=")
    flags+=("--backup-schedule=")
    two_word_flags+=("--backup-schedule")
    local_nonpersistent_flags+=("--backup-schedule")
    local_nonpersistent_flags+=("--backup-schedule=")
    flags+=("--instances=")
    two_word_flags+=("--instances")
    local_nonpersistent_flags+=("--instances")
    local_nonpersistent_flags+=("--instances=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--storage=")
    two_word_flags+=("--storage")
    local_nonpersistent_flags+=("--storage")
    local_nonpersistent_flags+=("--storage=")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_database()
{
    last_command="platformctl_database"

    command_aliases=()

    commands=()
    commands+=("backups")
    commands+=("create")
    commands+=("delete")
    commands+=("finalize")
    commands+=("restore")
    commands+=("status")
    commands+=("update")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_dev()
{
    last_command="platformctl_dev"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--bootstrap")
    local_nonpersistent_flags+=("--bootstrap")
    flags+=("--database-tunnel")
    local_nonpersistent_flags+=("--database-tunnel")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_doctor()
{
    last_command="platformctl_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_help()
{
    last_command="platformctl_help"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    has_completion_function=1
    noun_aliases=()
}

_platformctl_init()
{
    last_command="platformctl_init"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--create-team")
    local_nonpersistent_flags+=("--create-team")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--template=")
    two_word_flags+=("--template")
    local_nonpersistent_flags+=("--template")
    local_nonpersistent_flags+=("--template=")
    flags+=("--with-database")
    local_nonpersistent_flags+=("--with-database")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_flag+=("--template=")
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_platform_down()
{
    last_command="platformctl_platform_down"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_platform_status()
{
    last_command="platformctl_platform_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_platform_up()
{
    last_command="platformctl_platform_up"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_platform_verify()
{
    last_command="platformctl_platform_verify"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_platform()
{
    last_command="platformctl_platform"

    command_aliases=()

    commands=()
    commands+=("down")
    commands+=("status")
    commands+=("up")
    commands+=("verify")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_portal()
{
    last_command="platformctl_portal"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--no-open")
    local_nonpersistent_flags+=("--no-open")
    flags+=("--port=")
    two_word_flags+=("--port")
    local_nonpersistent_flags+=("--port")
    local_nonpersistent_flags+=("--port=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_profile_list()
{
    last_command="platformctl_profile_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_profile()
{
    last_command="platformctl_profile"

    command_aliases=()

    commands=()
    commands+=("list")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_request_open()
{
    last_command="platformctl_request_open"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_request_status()
{
    last_command="platformctl_request_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_request_watch()
{
    last_command="platformctl_request_watch"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_request()
{
    last_command="platformctl_request"

    command_aliases=()

    commands=()
    commands+=("open")
    commands+=("status")
    commands+=("watch")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_service_finalize()
{
    last_command="platformctl_service_finalize"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--approval-revision=")
    two_word_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision=")
    flags+=("--deletion-request=")
    two_word_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_service_retire()
{
    last_command="platformctl_service_retire"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--acknowledge-data-loss")
    local_nonpersistent_flags+=("--acknowledge-data-loss")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--team=")
    two_word_flags+=("--team")
    local_nonpersistent_flags+=("--team")
    local_nonpersistent_flags+=("--team=")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_service()
{
    last_command="platformctl_service"

    command_aliases=()

    commands=()
    commands+=("finalize")
    commands+=("retire")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_team_create()
{
    last_command="platformctl_team_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-repository=")
    two_word_flags+=("--allow-repository")
    local_nonpersistent_flags+=("--allow-repository")
    local_nonpersistent_flags+=("--allow-repository=")
    flags+=("--cpu=")
    two_word_flags+=("--cpu")
    local_nonpersistent_flags+=("--cpu")
    local_nonpersistent_flags+=("--cpu=")
    flags+=("--memory=")
    two_word_flags+=("--memory")
    local_nonpersistent_flags+=("--memory")
    local_nonpersistent_flags+=("--memory=")
    flags+=("--owner=")
    two_word_flags+=("--owner")
    local_nonpersistent_flags+=("--owner")
    local_nonpersistent_flags+=("--owner=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_team_delete()
{
    last_command="platformctl_team_delete"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--acknowledge-data-loss")
    local_nonpersistent_flags+=("--acknowledge-data-loss")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_team_finalize()
{
    last_command="platformctl_team_finalize"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--approval-revision=")
    two_word_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision")
    local_nonpersistent_flags+=("--approval-revision=")
    flags+=("--deletion-request=")
    two_word_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request")
    local_nonpersistent_flags+=("--deletion-request=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_team_list()
{
    last_command="platformctl_team_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_team_status()
{
    last_command="platformctl_team_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_team_update()
{
    last_command="platformctl_team_update"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-repository=")
    two_word_flags+=("--allow-repository")
    local_nonpersistent_flags+=("--allow-repository")
    local_nonpersistent_flags+=("--allow-repository=")
    flags+=("--cpu=")
    two_word_flags+=("--cpu")
    local_nonpersistent_flags+=("--cpu")
    local_nonpersistent_flags+=("--cpu=")
    flags+=("--memory=")
    two_word_flags+=("--memory")
    local_nonpersistent_flags+=("--memory")
    local_nonpersistent_flags+=("--memory=")
    flags+=("--owner=")
    two_word_flags+=("--owner")
    local_nonpersistent_flags+=("--owner")
    local_nonpersistent_flags+=("--owner=")
    flags+=("--plan")
    local_nonpersistent_flags+=("--plan")
    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_team()
{
    last_command="platformctl_team"

    command_aliases=()

    commands=()
    commands+=("create")
    commands+=("delete")
    commands+=("finalize")
    commands+=("list")
    commands+=("status")
    commands+=("update")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_version()
{
    last_command="platformctl_version"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_platformctl_root_command()
{
    last_command="platformctl"

    command_aliases=()

    commands=()
    commands+=("app")
    commands+=("cluster")
    commands+=("completion")
    commands+=("config")
    commands+=("context")
    commands+=("database")
    commands+=("dev")
    commands+=("doctor")
    commands+=("help")
    commands+=("init")
    commands+=("platform")
    commands+=("portal")
    commands+=("profile")
    commands+=("request")
    commands+=("service")
    commands+=("team")
    commands+=("version")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--context=")
    two_word_flags+=("--context")
    flags+=("--no-color")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    flags+=("--quiet")
    flags+=("-q")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

__start_platformctl()
{
    local cur prev words cword split
    declare -A flaghash 2>/dev/null || :
    declare -A aliashash 2>/dev/null || :
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion -s || return
    else
        __platformctl_init_completion -n "=" || return
    fi

    local c=0
    local flag_parsing_disabled=
    local flags=()
    local two_word_flags=()
    local local_nonpersistent_flags=()
    local flags_with_completion=()
    local flags_completion=()
    local commands=("platformctl")
    local command_aliases=()
    local must_have_one_flag=()
    local must_have_one_noun=()
    local has_completion_function=""
    local last_command=""
    local nouns=()
    local noun_aliases=()

    __platformctl_handle_word
}

if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_platformctl platformctl
else
    complete -o default -o nospace -F __start_platformctl platformctl
fi

# ex: ts=4 sw=4 et filetype=sh
