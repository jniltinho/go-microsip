# Run inside the Windows VM over WinRM: the milestone artifacts on a clean Windows. Prints one
# "vm-check:" line per step. The window and the audio test run in the logged-on desktop session
# through a scheduled task, because a WinRM session has no desktop.
param([Parameter(Mandatory = $true)][string]$Version, [int]$Seconds = 300)
$ErrorActionPreference = 'Stop'
$work = 'C:\g0work'
New-Item -ItemType Directory -Force $work | Out-Null
Copy-Item "C:\dist\go-microsip-$Version-windows-amd64.exe" "$work\go-microsip.exe" -Force
Copy-Item 'C:\dist\g0\audio.test.exe' "$work\audio.test.exe" -Force

$os = Get-CimInstance Win32_OperatingSystem
"vm-check: $($os.Caption) $($os.Version)"
$wv = (Get-ItemProperty 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' -ErrorAction SilentlyContinue).pv
"vm-check: WebView2 runtime $wv (part of Windows; nothing was installed)"

# 1. Headless modes: no desktop in this session.
cmd /c "`"$work\go-microsip.exe`" version > `"$work\version.txt`" 2>&1"
if ($LASTEXITCODE -ne 0) { "vm-check: FAIL version exit $LASTEXITCODE"; exit 1 }
cmd /c "`"$work\go-microsip.exe`" doctor --json > `"$work\doctor.txt`" 2>&1"
if ($LASTEXITCODE -ne 0) { "vm-check: FAIL doctor exit $LASTEXITCODE"; exit 1 }
"vm-check: $((Get-Content "$work\version.txt") -join ' ')"
"vm-check: doctor $((Get-Content "$work\doctor.txt") -join '' -replace '\s', '')"

# Run a command line in the desktop session, elevated, and return its exit code.
function Invoke-Desktop([string]$Name, [string[]]$Lines, [int]$Timeout) {
    $rc = "$work\$Name.rc"
    $log = "$work\$Name.log"
    Remove-Item $rc, $log -ErrorAction SilentlyContinue
    $bat = "$work\$Name.cmd"
    $body = @('@echo off') + $Lines[0..($Lines.Count - 2)] + @("$($Lines[-1]) > `"$log`" 2>&1", "echo %errorlevel% > `"$rc`"")
    Set-Content -Encoding ascii $bat $body
    # Through cmd: schtasks warns on stderr about the start time, which PowerShell would raise.
    cmd /c "schtasks /create /tn g0-$Name /tr `"$bat`" /sc once /st 00:00 /ru vagrant /it /rl highest /f >nul 2>&1"
    if ($LASTEXITCODE -ne 0) { return -2 }
    cmd /c "schtasks /run /tn g0-$Name >nul 2>&1"
    $deadline = (Get-Date).AddSeconds($Timeout)
    while (-not (Test-Path $rc) -and (Get-Date) -lt $deadline) {
        # While it runs: the TCP ports the softphone itself listens on (the window needs none).
        foreach ($p in @(Get-Process go-microsip -ErrorAction SilentlyContinue)) {
            $script:Samples++
            foreach ($l in @(Get-NetTCPConnection -State Listen -OwningProcess $p.Id -ErrorAction SilentlyContinue)) {
                $script:Listening["$($l.LocalAddress):$($l.LocalPort)"] = $true
            }
        }
        Start-Sleep -Milliseconds 300
    }
    cmd /c "schtasks /delete /tn g0-$Name /f >nul 2>&1"
    if (-not (Test-Path $rc)) { return -1 }
    return [int]((Get-Content $rc) -join '').Trim()
}

# 2. The audio adapter on the Windows audio service.
$code = Invoke-Desktop 'audio' @('set GO_MICROSIP_AUDIO_TEST=1', "set GO_MICROSIP_AUDIO_SECONDS=$Seconds",
    "`"$work\audio.test.exe`" -test.v -test.timeout 20m -test.run TestWASAPI") ($Seconds + 120)
Get-Content "$work\audio.log" -ErrorAction SilentlyContinue | Where-Object { $_ -match 'wasapi_windows_test|^(--- |PASS|FAIL|ok|panic)' } | ForEach-Object { "vm-check: audio: $_" }
$failed = ''
if ($code -ne 0) { "vm-check: audio: FAILED (exit $code); the window is still checked"; $failed = 'audio' }
else { 'vm-check: audio adapter OK on the Windows audio service' }

# 3. The window on WebView2, and the page's confinement as the self-test finds it.
$script:Samples = 0
$script:Listening = @{}
$code = Invoke-Desktop 'selftest' @("`"$work\go-microsip.exe`" --self-test") 150
if ($code -ne 0) {
    "vm-check: FAIL self-test exit $code"
    Get-Content "$work\selftest.log" -Tail 20 -ErrorAction SilentlyContinue
    exit 1
}
'vm-check: window opened on WebView2 and the page called the backend'
Get-Content "$work\selftest.log" -ErrorAction SilentlyContinue | Where-Object { $_ -match '^self-test:' } | ForEach-Object { "vm-check: $_" }
if ($script:Samples -eq 0) { 'vm-check: listening ports: the softphone was not seen running, so not checked' }
elseif ($script:Listening.Count -gt 0) { "vm-check: listening ports: the softphone listens on $($script:Listening.Keys -join ', ')"; $failed = "$failed listening" }
else { "vm-check: listening ports: none in $($script:Samples) looks at the running softphone" }
if ($failed) { "vm-check: FAIL $($os.Caption): $failed"; exit 1 }
"vm-check: PASS $($os.Caption)"
