#!/bin/bash
# Shell integration for deploy-manager terminal
# Emits OSC 133 sequences for command boundary detection

__dm_prompt_start() { printf '\e]133;A\e\\'; }
__dm_command_start() { printf '\e]133;B\e\\'; }
__dm_command_end() { printf '\e]133;C;%d\e\\' "$?"; }
__dm_output_start() { printf '\e]133;D\e\\'; }
__dm_cwd() { printf '\e]7;file://%s%s\e\\' "$(hostname)" "$(pwd)"; }

if [ -n "$ZSH_VERSION" ]; then
    __dm_precmd() {
        __dm_command_end
        __dm_output_start
        __dm_cwd
        __dm_prompt_start
    }
    __dm_preexec() { __dm_command_start; }
    precmd_functions+=(__dm_precmd)
    preexec_functions+=(__dm_preexec)
elif [ -n "$BASH_VERSION" ]; then
    __dm_prompt_cmd() {
        local exit_code=$?
        __dm_command_end
        __dm_output_start
        __dm_cwd
        __dm_prompt_start
        return $exit_code
    }
    PROMPT_COMMAND="__dm_prompt_cmd;${PROMPT_COMMAND}"
    trap '__dm_command_start' DEBUG
fi
