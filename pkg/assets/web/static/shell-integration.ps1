# Shell integration for deploy-manager terminal (PowerShell)
# Emits OSC 133 sequences for command boundary detection

# Set UTF-8 encoding silently
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
[Console]::InputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8

$Global:__dm_last_exit = 0

function __dm_PromptStart { "$([char]27)]133;A$([char]27)\" }
function __dm_CommandStart { "$([char]27)]133;B$([char]27)\" }
function __dm_CommandEnd { "$([char]27)]133;C;$($Global:__dm_last_exit)$([char]27)\" }
function __dm_OutputStart { "$([char]27)]133;D$([char]27)\" }
function __dm_Cwd { "$([char]27)]7;file://$env:COMPUTERNAME$($executionContext.SessionState.Path.CurrentLocation.Path)$([char]27)\" }

function Prompt {
    $Global:__dm_last_exit = $global:LASTEXITCODE
    $path = $PWD.ProviderPath
    if ([string]::IsNullOrEmpty($path)) {
        $path = $executionContext.SessionState.Path.CurrentLocation.Path
    }
    "$(__dm_CommandEnd)$(__dm_OutputStart)$(__dm_Cwd)$(__dm_PromptStart)PS $path$('>' * ($nestedPromptLevel + 1)) "
}

# Emit initial prompt start
__dm_PromptStart
