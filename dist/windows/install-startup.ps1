# Installs seasoned.exe to %LOCALAPPDATA%\seasoned and wires it up to start
# at user login. Run from the directory that contains your built
# seasoned.exe, or pass -SourcePath to point at it.
#
# Two install modes:
#   -Mode Shortcut  (default) — drops a hidden-window shortcut into
#                   shell:startup. Simplest, no admin required.
#   -Mode Task                — registers a Scheduled Task triggered
#                   "At log on". More robust if the Startup folder
#                   is managed or disabled by policy.

[CmdletBinding()]
param(
    [string]$SourcePath = (Join-Path (Get-Location) "seasoned.exe"),
    [ValidateSet("Shortcut","Task")]
    [string]$Mode = "Shortcut"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path $SourcePath)) {
    throw "seasoned.exe not found at $SourcePath. Build it first or pass -SourcePath."
}

$installDir = Join-Path $env:LOCALAPPDATA "seasoned"
New-Item -ItemType Directory -Path $installDir -Force | Out-Null

$exePath = Join-Path $installDir "seasoned.exe"
Copy-Item -Path $SourcePath -Destination $exePath -Force
Write-Host "Installed: $exePath"

switch ($Mode) {
    "Shortcut" {
        $startup = [Environment]::GetFolderPath("Startup")
        $lnk = Join-Path $startup "seasoned.lnk"

        $shell = New-Object -ComObject WScript.Shell
        $s = $shell.CreateShortcut($lnk)
        $s.TargetPath = $exePath
        $s.Arguments = "daemon"
        $s.WorkingDirectory = $installDir
        $s.WindowStyle = 7  # minimized; Windows has no "hidden" for shortcuts
        $s.Description = "seasoned-wallpaper daemon"
        $s.Save()
        Write-Host "Startup shortcut: $lnk"
    }

    "Task" {
        $taskName = "seasoned-wallpaper"
        $action   = New-ScheduledTaskAction -Execute $exePath -Argument "daemon"
        $trigger  = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
        $settings = New-ScheduledTaskSettingsSet `
            -AllowStartIfOnBatteries `
            -DontStopIfGoingOnBatteries `
            -StartWhenAvailable `
            -RestartCount 3 `
            -RestartInterval (New-TimeSpan -Minutes 1)
        $principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType Interactive -RunLevel Limited

        Register-ScheduledTask -TaskName $taskName `
            -Action $action -Trigger $trigger -Settings $settings `
            -Principal $principal -Force | Out-Null
        Write-Host "Scheduled Task registered: $taskName"
    }
}

Write-Host "Done. The daemon will start at next login. To start it now:"
Write-Host "  & '$exePath' daemon"
