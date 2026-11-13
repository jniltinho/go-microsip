# Run inside the Windows VM over WinRM: the milestone artifacts on a clean Windows. Prints one
# "vm-check:" line per step. The window and the audio test run in the logged-on desktop session
# through a scheduled task, because a WinRM session has no desktop.
param([Parameter(Mandatory = $true)][string]$Version, [int]$Seconds = 300, [string]$Pbx = '')
$ErrorActionPreference = 'Stop'
$work = 'C:\g0work'
New-Item -ItemType Directory -Force $work | Out-Null
# A VM kept up between runs may still have the softphone of the run before: it is ended, or its
# file could not be replaced.
Get-Process go-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Milliseconds 500
Copy-Item "C:\dist\go-microsip-$Version-windows-amd64.exe" "$work\go-microsip.exe" -Force
Copy-Item 'C:\dist\g0\audio.test.exe' "$work\audio.test.exe" -Force
Copy-Item 'C:\dist\g0\lock.test.exe' "$work\lock.test.exe" -Force
Copy-Item 'C:\dist\g0\keyring.test.exe' "$work\keyring.test.exe" -Force
Copy-Item 'C:\dist\g0\control.test.exe' "$work\control.test.exe" -Force
Copy-Item 'C:\dist\g0\notify.test.exe' "$work\notify.test.exe" -Force
if (Test-Path 'C:\dist\g0\mcp.test.exe') { Copy-Item 'C:\dist\g0\mcp.test.exe' "$work\mcp.test.exe" -Force }

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

# The .zip as a user gets it: extracted, it holds the program, the license and the notices, and
# that program says the same version as the .exe beside it.
$failed = ''
try {
    Remove-Item -Recurse -Force "$work\zip" -ErrorAction SilentlyContinue
    Expand-Archive "C:\dist\go-microsip-$Version-windows-amd64.zip" "$work\zip"
    $inside = @(Get-ChildItem "$work\zip" -Recurse -File)
    $program = $inside | Where-Object Name -eq 'go-microsip.exe' | Select-Object -First 1
    cmd /c "`"$($program.FullName)`" version > `"$work\zipversion.txt`" 2>&1"
    $same = ((Get-Content "$work\zipversion.txt") -join ' ') -eq ((Get-Content "$work\version.txt") -join ' ')
    $names = ($inside | ForEach-Object Name | Sort-Object) -join ', '
    if ($LASTEXITCODE -eq 0 -and $same -and $names -eq 'go-microsip.exe, LICENSE.txt, THIRD_PARTY_NOTICES.txt') {
        "vm-check: the .zip extracts to $names, and its program says the same version"
    } else { "vm-check: zip: FAILED: it holds $names; its program's version exit $LASTEXITCODE, the same as the .exe: $same"; $failed = "$failed zip" }
} catch { "vm-check: zip: FAILED: $($_.Exception.Message)"; $failed = "$failed zip" }

# A data folder that is not an absolute path (WIN-09): the import that would write the database
# names the problem and creates nothing, in the folder it was started in or anywhere it says.
try {
    $probe = "$work\nodata"
    Remove-Item -Recurse -Force $probe -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force $probe | Out-Null
    Set-Content -Encoding ascii "$probe\in.toml" @('schema = 1', '[account]', 'username = "1001"', 'domain = "pbx.invalid"', 'server = "pbx.invalid"')
    cmd /c "cd /d `"$probe`" && set LOCALAPPDATA=relative&& `"$work\go-microsip.exe`" config import in.toml --apply > `"$work\nodata.txt`" 2>&1"
    $code = $LASTEXITCODE
    $said = (Get-Content "$work\nodata.txt" -ErrorAction SilentlyContinue) -join ' '
    $left = @(Get-ChildItem $probe -Recurse -Force | Where-Object Name -ne 'in.toml')
    if ($code -ne 0 -and $said -match 'no data directory' -and $said -match 'LOCALAPPDATA' -and $left.Count -eq 0) {
        "vm-check: with LOCALAPPDATA not an absolute path, config import --apply ends with $code, names it, and creates nothing"
    } else { "vm-check: data folder: FAILED: exit $code, said: $said; left behind: $($left.Name -join ', ')"; $failed = "$failed datafolder" }
} catch { "vm-check: data folder: FAILED: $($_.Exception.Message)"; $failed = "$failed datafolder" }

# The instance lock, which the database of settings, contacts and history is opened under.
cmd /c "`"$work\lock.test.exe`" -test.v > `"$work\lock.txt`" 2>&1"
$lock = $LASTEXITCODE
Get-Content "$work\lock.txt" -ErrorAction SilentlyContinue | Where-Object { $_ -match '^(--- |PASS|FAIL|ok|panic)' } | ForEach-Object { "vm-check: lock: $_" }
if ($lock -ne 0) { "vm-check: FAIL the instance lock (exit $lock)"; exit 1 }
'vm-check: the instance lock is exclusive and released on Windows'

# The control transport: the socket of the user's own directory, one owner, the system's word on
# which process is at the other end and whose it is.
cmd /c "`"$work\control.test.exe`" -test.v -test.run `"TestAClientReachesTheOwnerOnWindows|TestGrantFilesOnWindows`" > `"$work\control.txt`" 2>&1"
$control = $LASTEXITCODE
Get-Content "$work\control.txt" -ErrorAction SilentlyContinue | Where-Object { $_ -match '_test.go|^(--- |PASS|FAIL|ok|panic)' } | ForEach-Object { "vm-check: control: $_" }
if ($control -ne 0) { "vm-check: FAIL the control transport (exit $control)"; exit 1 }
'vm-check: the control transport serves one owner and names its peers on Windows'

# Run a command line in the desktop session, elevated, and return its exit code.
function Invoke-Desktop([string]$Name, [string[]]$Lines, [int]$Timeout) {
    $rc = "$work\$Name.rc"
    $log = "$work\$Name.log"
    Remove-Item $rc, $log -ErrorAction SilentlyContinue
    $bat = "$work\$Name.cmd"
    # The lines before the last one run as they are; a single line has none (0..-1 would name it twice).
    $head = @()
    if ($Lines.Count -gt 1) { $head = $Lines[0..($Lines.Count - 2)] }
    $body = @('@echo off') + $head + @("$($Lines[-1]) > `"$log`" 2>&1", "echo %errorlevel% > `"$rc`"")
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
if ($code -ne 0) { "vm-check: audio: FAILED (exit $code); the window is still checked"; $failed = 'audio' }
else { 'vm-check: audio adapter OK on the Windows audio service' }

# 2b. The keyring: the Credential Manager of the logged-on user, where a typed password is kept.
# In the desktop session: a WinRM logon has no credential store of its own.
$code = Invoke-Desktop 'keyring' @('set GO_MICROSIP_KEYRING_TEST=keyring',
    "`"$work\keyring.test.exe`" -test.v -test.run TestKeyringStoresReplacesAndDeletes") 120
Get-Content "$work\keyring.log" -ErrorAction SilentlyContinue | Where-Object { $_ -match 'keyring_test|^(--- |PASS|FAIL|ok|panic)' } | ForEach-Object { "vm-check: keyring: $_" }
if ($code -ne 0) { "vm-check: keyring: FAILED (exit $code)"; $failed = "$failed keyring" }
else { 'vm-check: the Credential Manager stores, replaces and deletes a password' }

# 2c. A stored password is the user's, not the application folder's: one copy of the program
# leaves it, that copy's folder is removed, and another copy finds it.
New-Item -ItemType Directory -Force "$work\app1", "$work\app2" | Out-Null
Copy-Item "$work\keyring.test.exe" "$work\app1\keyring.test.exe" -Force
$code = Invoke-Desktop 'leave' @('set GO_MICROSIP_KEYRING_TEST=leave',
    "`"$work\app1\keyring.test.exe`" -test.v -test.run TestAStoredPasswordOutlivesTheRunThatLeftIt") 120
Remove-Item -Recurse -Force "$work\app1"
Copy-Item "$work\keyring.test.exe" "$work\app2\keyring.test.exe" -Force
$found = Invoke-Desktop 'find' @('set GO_MICROSIP_KEYRING_TEST=find',
    "`"$work\app2\keyring.test.exe`" -test.v -test.run TestAStoredPasswordOutlivesTheRunThatLeftIt") 120
Get-Content "$work\find.log" -ErrorAction SilentlyContinue | Where-Object { $_ -match 'keyring_test|^(--- |FAIL|panic)' } | ForEach-Object { "vm-check: keyring: $_" }
if ($code -ne 0 -or $found -ne 0 -or (Test-Path "$work\app1")) { "vm-check: keyring: FAILED: a password did not outlive its application folder (leave $code, find $found)"; $failed = "$failed keyring-folder" }
else { 'vm-check: a stored password outlives the removal of the application folder that stored it' }

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

# 4. Every skin on WebView2: the window started in each one draws it with its bundled font. A
# picture of each is left beside the artifacts (C:\dist\vm).
New-Item -ItemType Directory -Force 'C:\dist\vm' | Out-Null
$code = Invoke-Desktop 'skins' @("powershell -NoProfile -ExecutionPolicy Bypass -File C:\vagrant\skins.ps1 -Exe `"$work\go-microsip.exe`" -Work `"$work`" -Shots C:\dist\vm") 900
Get-Content "$work\skins.log" -ErrorAction SilentlyContinue | Where-Object { $_ -match '^skin:' } | ForEach-Object { "vm-check: $_" }
if ($code -ne 0) { "vm-check: skins: FAILED (exit $code)"; Get-Content "$work\skins.log" -Tail 8 -ErrorAction SilentlyContinue; $failed = "$failed skins" }
else { 'vm-check: the seven skins drawn on WebView2, each with its bundled font' }

# 5. The desktop's side of the window: links, the control socket, a second start, the tray.
$code = Invoke-Desktop 'desktop' @("powershell -NoProfile -ExecutionPolicy Bypass -File C:\vagrant\desktop.ps1 -Exe `"$work\go-microsip.exe`" -Work `"$work`" -ControlTest `"$work\control.test.exe`" -NotifyTest `"$work\notify.test.exe`" -McpTest `"$work\mcp.test.exe`" -Shots C:\dist\vm") 480
Get-Content "$work\desktop.log" -ErrorAction SilentlyContinue | Where-Object { $_ -match '^desktop:' } | ForEach-Object { "vm-check: $_" }
if ($code -ne 0) { "vm-check: desktop: FAILED (exit $code)"; Get-Content "$work\desktop.log" -Tail 6 -ErrorAction SilentlyContinue; $failed = "$failed desktop" }
else { 'vm-check: links, agent control, the second start and the tray pass on Windows; the notification reaches Windows' }

# 5b. Where the window left its data: the database under LOCALAPPDATA, which does not roam, and no
# configuration file written by a window that was only looked at.
$database = "$env:LOCALAPPDATA\go-microsip\go-microsip.sqlite3"
$written = Test-Path "$env:APPDATA\go-microsip\config.toml"
if ((Test-Path $database) -and -not $written) { "vm-check: the window's database is $database; no config.toml was written under APPDATA" }
else { "vm-check: folders: FAILED: database there: $(Test-Path $database); a config.toml under APPDATA: $written"; $failed = "$failed folders" }

# 6. The call walkthrough, when run.sh has the lab PBX up on the VMs' own network (GO_VM_LAB=1).
if ($Pbx) {
    if (-not (Test-Path 'C:\lab\credentials.json')) { 'vm-check: call: FAILED: the accounts of the lab are not shared with this VM (vagrant reload windows11)'; $failed = "$failed call" }
    else {
        $code = Invoke-Desktop 'call' @("powershell -NoProfile -ExecutionPolicy Bypass -File C:\vagrant\call.ps1 -Exe `"$work\go-microsip.exe`" -Work `"$work`" -Pbx $Pbx -Shots C:\dist\vm") 600
        Get-Content "$work\call.log" -ErrorAction SilentlyContinue | Where-Object { $_ -match '^desktop:' } | ForEach-Object { "vm-check: $_" }
        if ($code -ne 0) { "vm-check: call: FAILED (exit $code)"; Get-Content "$work\call.log" -Tail 6 -ErrorAction SilentlyContinue; $failed = "$failed call" }
        else { 'vm-check: the window registers, calls the lab PBX over UDP and TCP, is called by it, and hangs up, on Windows' }
    }
}
if ($failed) { "vm-check: FAIL $($os.Caption): $failed"; exit 1 }
"vm-check: PASS $($os.Caption)"
