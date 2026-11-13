# Start-up time and memory of go-microsip's .exe and of the Rust candidate's on Windows (task
# 7.3), measured the same way, one after the other, each start with new, empty folders for its
# settings and data and no account.
#   powershell -File C:\vagrant\measure.ps1 [-Version 0.2.0-rc.1] [-Runs 10]      over WinRM
# Over WinRM there is no desktop: the script starts itself again in the logged-on desktop
# session through a scheduled task, as check.ps1 does, and prints what that run wrote.
#
# Two moments are timed from the launch: the process has a window, and, for go-microsip, its
# window reports its first drawn page (the Rust candidate has no such report). Memory is taken
# 20 s after the last start: the working set and the private bytes of the process and of every
# process it started (go-microsip's web view runs in processes of its own).
param([string]$Version = '0.2.0-rc.1', [int]$Runs = 10, [switch]$Desktop)
$ErrorActionPreference = 'Stop'
$work = 'C:\g0work'
$log = "$work\measure.log"
if (-not $Desktop) {
    New-Item -ItemType Directory -Force $work | Out-Null
    Get-Process go-microsip, rust-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Milliseconds 500
    Copy-Item "C:\dist\go-microsip-$Version-windows-amd64.exe" "$work\go-microsip.exe" -Force
    Copy-Item 'C:\dist\g0\rust-microsip.exe' "$work\rust-microsip.exe" -Force
    Remove-Item $log, "$work\measure.rc" -ErrorAction SilentlyContinue
    Set-Content -Encoding ascii "$work\measure.cmd" @('@echo off',
        "powershell -NoProfile -ExecutionPolicy Bypass -File C:\vagrant\measure.ps1 -Version $Version -Runs $Runs -Desktop > `"$log`" 2>&1",
        "echo %errorlevel% > `"$work\measure.rc`"")
    cmd /c "schtasks /create /tn g0-measure /tr `"$work\measure.cmd`" /sc once /st 00:00 /ru vagrant /it /rl highest /f >nul 2>&1"
    cmd /c "schtasks /run /tn g0-measure >nul 2>&1"
    $deadline = (Get-Date).AddSeconds(900)
    while (-not (Test-Path "$work\measure.rc") -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 2 }
    Get-Content $log -ErrorAction SilentlyContinue
    cmd /c "schtasks /delete /tn g0-measure /f >nul 2>&1"
    if (-not (Test-Path "$work\measure.rc")) { 'measure: FAILED: the measurement did not end'; exit 1 }
    exit [int](Get-Content "$work\measure.rc")
}

$os = Get-CimInstance Win32_OperatingSystem
"measure: $($os.Caption) $($os.Version), $([Environment]::ProcessorCount) processors, $([int]($os.TotalVisibleMemorySize / 1024)) MB"
function Leave() {
    Get-Process go-microsip, rust-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Milliseconds 1200
}
# One start, with folders of its own. Returns the process and the two moments, in milliseconds.
function Once([string]$Name) {
    $home1 = "$work\measure-profile"
    Remove-Item $home1 -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force "$home1\roaming", "$home1\local" | Out-Null
    $trace = "$work\measure.jsonl"
    Remove-Item $trace -ErrorAction SilentlyContinue
    $env:APPDATA = "$home1\roaming"
    $env:LOCALAPPDATA = "$home1\local"
    $watch = [Diagnostics.Stopwatch]::StartNew()
    if ($Name -eq 'go-microsip') { $p = Start-Process "$work\$Name.exe" -ArgumentList '--ui-trace', "`"$trace`"" -PassThru }
    else { $p = Start-Process "$work\$Name.exe" -PassThru }
    $window = $null
    $drawn = $null
    while ($watch.ElapsedMilliseconds -lt 90000 -and (-not $window -or ($Name -eq 'go-microsip' -and -not $drawn))) {
        if (-not $window) { $p.Refresh(); if ($p.HasExited) { break }; if ($p.MainWindowHandle -ne 0) { $window = $watch.ElapsedMilliseconds } }
        if ($Name -eq 'go-microsip' -and -not $drawn -and (Test-Path $trace) -and (Select-String -Path $trace -Pattern '"page"' -Quiet)) { $drawn = $watch.ElapsedMilliseconds }
        Start-Sleep -Milliseconds 10
    }
    return @{ Process = $p; Window = $window; Drawn = $drawn }
}
function Stats($Numbers) {
    $v = @($Numbers | Sort-Object)
    "median $($v[[int][Math]::Floor(($v.Count - 1) / 2)]) ms, least $($v[0]) ms, most $($v[-1]) ms, $($v.Count) starts"
}
$failed = 0
foreach ($name in 'go-microsip', 'rust-microsip') {
    "measure: ${name}: $((Get-Item "$work\$name.exe").Length) bytes"
    Leave
    $windows = @()
    $pages = @()
    $last = $null
    foreach ($n in 0..$Runs) {
        $last = Once $name
        if (-not $last.Window) { break }
        if ($name -eq 'go-microsip' -and -not $last.Drawn) { $last.Window = $null; break } # a window whose page was never drawn
        if ($n -eq 0) { "measure: ${name}: first start: $($last.Window) ms to a window$(if ($last.Drawn) { ", $($last.Drawn) ms to the first drawn page" })" }
        else { $windows += $last.Window; if ($last.Drawn) { $pages += $last.Drawn } }
        if ($n -lt $Runs) { Leave }
    }
    if (-not $last.Window) {
        "measure: ${name}: NOT MEASURED: a start opened no window, or drew no page, here (exit $($last.Process.ExitCode))"
        Leave
        continue
    }
    "measure: ${name}: to a window: $(Stats $windows)"
    if ($pages.Count) { "measure: ${name}: to the first drawn page: $(Stats $pages)" }
    Start-Sleep -Seconds 20
    $all = Get-CimInstance Win32_Process
    $tree = @($last.Process.Id)
    do {
        $more = @($all | Where-Object { $tree -contains $_.ParentProcessId -and $tree -notcontains $_.ProcessId } | ForEach-Object { $_.ProcessId })
        $tree += $more
    } while ($more.Count)
    $mine = @($all | Where-Object { $tree -contains $_.ProcessId })
    $set = ($mine | Measure-Object -Property WorkingSetSize -Sum).Sum
    $private = ($mine | Measure-Object -Property PrivatePageCount -Sum).Sum
    "measure: ${name}: memory 20 s after the start: $([int]($set / 1MB)) MB working set and $([int]($private / 1MB)) MB private in $($mine.Count) processes"
    Leave
}
exit $failed
