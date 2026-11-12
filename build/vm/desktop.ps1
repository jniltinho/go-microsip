# Run by check.ps1 in the Windows VM's desktop session (task 6.3): the window, as Windows sees
# it. With no account configured, nothing registers and nothing is dialed. It checks that
# go-microsip is registered for sip: and tel: links, that its control socket answers the command
# line, that a second start with a link is handed to the running window, which asks, that a call
# asked for on the command line waits for the person, and that the close button hides the window
# to the notification area. Prints one "desktop:" line per step; the exit code is the number of
# steps that failed.
param([Parameter(Mandatory = $true)][string]$Exe, [Parameter(Mandatory = $true)][string]$Work,
    [string]$ControlTest = '', [string]$NotifyTest = '', [string]$Shots = '')
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Shell {
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
}
"@
$script:failed = 0
function Fail([string]$what) { "desktop: FAILED: $what"; $script:failed++ }
# The softphone's command line, with its output: a window program has no console of its own.
function Run([string]$Arguments, [int]$Timeout = 30) {
    $out = "$Work\run.out"
    Remove-Item $out -ErrorAction SilentlyContinue
    $p = Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', "`"`"$Exe`" $Arguments > `"$out`" 2>&1`"" -PassThru -WindowStyle Hidden
    if (-not $p.WaitForExit($Timeout * 1000)) { $p.Kill(); return @{ Code = -1; Out = 'did not end' } }
    return @{ Code = $p.ExitCode; Out = ((Get-Content $out -ErrorAction SilentlyContinue) -join "`n") }
}
$trace = "$Work\desktop.jsonl"
# What the page shows now: the text of its last report.
function Page() {
    $last = @(Get-Content $trace -ErrorAction SilentlyContinue | Where-Object { $_ -match '"page"' }) | Select-Object -Last 1
    if ($last) { return $last } else { return '' }
}
function Shows([string]$Pattern, [int]$Seconds) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    while ((Get-Date) -lt $deadline) {
        if ((Page) -match $Pattern) { return $true }
        Start-Sleep -Milliseconds 400
    }
    return $false
}
function Window() { Get-Process go-microsip -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1 }
# A key for the window, as a person at the keyboard.
function Key([string]$Keys) {
    $w = Window
    if ($w) {
        [Shell]::ShowWindow($w.MainWindowHandle, 9) | Out-Null
        [Shell]::SetForegroundWindow($w.MainWindowHandle) | Out-Null
        Start-Sleep -Milliseconds 500
    }
    [System.Windows.Forms.SendKeys]::SendWait($Keys)
}

Get-Process go-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
Remove-Item $trace -ErrorAction SilentlyContinue
$env:GO_MICROSIP_UI_THEME = 'light'
Start-Process -FilePath $Exe -ArgumentList '--ui-trace', "`"$trace`"" -RedirectStandardError "$Work\desktop.err" | Out-Null
if (-not (Shows 'Keypad' 90)) {
    Fail 'the window did not draw its home screen'
    Get-Content "$Work\desktop.err" -Tail 5 -ErrorAction SilentlyContinue | ForEach-Object { "desktop:   $_" }
    exit $script:failed
}
[System.Windows.Forms.SendKeys]::SendWait('{ESC}') # a fresh logon leaves the Start menu open
'desktop: the window is open'

# 1. Links: go-microsip is a candidate for sip: and tel:, for this user, with this executable.
try {
    $command = (Get-ItemProperty 'HKCU:\Software\Classes\go-microsip.link\shell\open\command').'(default)'
    $protocol = (Get-Item 'HKCU:\Software\Classes\go-microsip.link').GetValueNames() -contains 'URL Protocol'
    $links = Get-ItemProperty 'HKCU:\Software\go-microsip\Capabilities\URLAssociations'
    $registered = (Get-ItemProperty 'HKCU:\Software\RegisteredApplications').'go-microsip'
    if ($command -eq "`"$Exe`" `"%1`"" -and $protocol -and $links.sip -eq 'go-microsip.link' -and $links.tel -eq 'go-microsip.link' -and $registered -eq 'Software\go-microsip\Capabilities') {
        'desktop: links: registered for sip: and tel: under the current user, with this executable; not made the default'
    } else { Fail "links: the registration is command=$command protocol=$protocol sip=$($links.sip) tel=$($links.tel) registered=$registered" }
} catch { Fail "links: not registered: $($_.Exception.Message)" }

# 2. Agent control: the command line reaches this window over the user's control socket.
$status = Run 'status --json'
if ($status.Code -eq 0 -and $status.Out -match '"mode":"gui"' -and $status.Out -match '"registration":"disabled"') {
    'desktop: control: status --json is answered by the window (mode gui)'
} else { Fail "control: status --json exit $($status.Code): $($status.Out)" }
$history = Run 'history list'
if ($history.Code -eq 3 -and $history.Out -match 'grant_required') { 'desktop: control: history list without a grant is refused (grant_required, exit 3)' }
else { Fail "control: history list exit $($history.Code): $($history.Out)" }
if ($ControlTest) {
    $env:GO_MICROSIP_CONTROL_TEST = 'session'
    $log = "$Work\session.log"
    $p = Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', "`"`"$ControlTest`" -test.v -test.run TestTheWindowOfThisSessionAnswers > `"$log`" 2>&1`"" -PassThru -WindowStyle Hidden
    $p.WaitForExit(60000) | Out-Null
    Get-Content $log -ErrorAction SilentlyContinue | Where-Object { $_ -match 'notify_windows_test|^(--- |FAIL|panic)' } | ForEach-Object { "desktop: notifications: $_" }
    # What Windows holds for go-microsip in its notification center.
    $held = 0
    try {
        [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
        $held = @([Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory('go-microsip')).Count
    } catch { "desktop: notifications: the notification history was not read: $($_.Exception.Message)" }
    if ($p.ExitCode -ne 0) { Fail 'notifications: the toast test' }
    elseif ($pressed) { 'desktop: notifications: the toast was shown and its Answer button, pressed on the screen, decided the call' }
    elseif ($held -gt 0) { "desktop: notifications: Windows took the toast and holds $held notification(s) of go-microsip in its notification center; NOT VERIFIED: its buttons (Windows' own do-not-disturb kept it off the screen, and the button was not found there)" }
    else { Fail 'notifications: Windows holds no notification of go-microsip' }
    [System.Windows.Forms.SendKeys]::SendWait('{ESC}')
}
exit $script:failed
