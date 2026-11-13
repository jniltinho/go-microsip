# Run by check.ps1 in the Windows VM's desktop session (task 6.3): the window, as Windows sees
# it. With no account configured, nothing registers and nothing is dialed. It checks that
# go-microsip is registered for sip: and tel: links, that its control socket answers the command
# line, that a second start with a link is handed to the running window, which asks, that a call
# asked for on the command line waits for the person, and that the close button hides the window
# to the notification area, and that a ringing call's notification reaches Windows. Prints one
# "desktop:" line per step; the exit code is the number of steps that failed.
param([Parameter(Mandatory = $true)][string]$Exe, [Parameter(Mandatory = $true)][string]$Work,
    [string]$ControlTest = '', [string]$NotifyTest = '', [string]$McpTest = '', [string]$Shots = '')
$ErrorActionPreference = 'Stop'
$trace = "$Work\desktop.jsonl"
. "$PSScriptRoot\window.ps1"

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
    Get-Content $log -ErrorAction SilentlyContinue | Where-Object { $_ -match 'session_test|^(--- |FAIL|panic)' } | ForEach-Object { "desktop: control: $_" }
    if ($p.ExitCode -ne 0) { Fail 'control: the session test' }
}
# 3. A second start with a link ends by itself, and the running window asks before it calls.
$link = Run 'tel:600'
if ($link.Code -eq 0 -and (Shows 'A link asks to call 600' 20)) {
    'desktop: single instance: a second start with tel:600 ended with 0 and the running window asks "A link asks to call 600. Call it?"'
} else { Fail "single instance: the second start exit $($link.Code): $($link.Out); the page shows: $((Page).Substring(0, [Math]::Min(300, (Page).Length)))" }
Key '{ESC}' # Cancel: nothing is dialed
if (-not (Shows '^(?!.*A link asks to call).*$' 10)) { Fail 'single instance: the question did not go away on Escape' }

# 3b. A sip: link, as the registered command hands it over, and one that is refused: it ends with
# 2, the refusal does not repeat it, and the window asks nothing.
$sip = Run 'sip:601@pbx.invalid'
if ($sip.Code -eq 0 -and (Shows 'A link asks to call 601' 20)) {
    'desktop: links: a second start with sip:601@pbx.invalid ended with 0 and the running window asks "A link asks to call 601" (the host is not part of it)'
} else { Fail "links: the sip: link exit $($sip.Code): $($sip.Out)" }
Key '{ESC}'
if (-not (Shows '^(?!.*A link asks to call).*$' 10)) { Fail 'links: the question of the sip: link did not go away on Escape' }
$refused = Run 'sip:602@pbx.invalid;transport=tcp'
if ($refused.Code -eq 2 -and $refused.Out -match 'not supported' -and $refused.Out -notmatch '602' -and -not (Shows 'A link asks to call' 3)) {
    'desktop: links: a link with parameters is refused (exit 2), the refusal does not repeat it, and the window asks nothing'
} else { Fail "links: a link with parameters: exit $($refused.Code): $($refused.Out)" }
if (@(Get-Process go-microsip -ErrorAction SilentlyContinue).Count -ne 1) { Fail "single instance: $(@(Get-Process go-microsip).Count) softphone processes run" }

# 4. A call asked for on the command line waits for the person; Escape denies it.
# A key of this run: the VM is kept between runs, and a key of an earlier one is stale by now.
$key = "vm-dial-$([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())"
$dial = Run "call dial 600 --key $key --json"
if ($dial.Code -eq 3 -and $dial.Out -match '"status":"approval_required"' -and $dial.Out -match '"operation_id":"(op-[A-Za-z0-9-]+)"') {
    $operation = $Matches[1]
    if (Shows 'asks to:.*call 600' 20) { 'desktop: control: call dial 600 waits (approval_required, exit 3) and the window shows who asks and for what' }
    else { Fail 'control: the window does not show the request' }
    Key '{ESC}'
    Start-Sleep -Seconds 2
    $after = Run "operation get $operation"
    if ($after.Code -eq 0 -and $after.Out -match '"state": "denied"') { 'desktop: control: Escape denied it, and operation get says denied' }
    else { Fail "control: operation get after Escape exit $($after.Code): $($after.Out)" }
} else { Fail "control: call dial exit $($dial.Code): $($dial.Out)" }

if ($Shots) {
    try {
        $w = Window
        [Shell]::ShowWindow($w.MainWindowHandle, 9) | Out-Null
        [Shell]::SetForegroundWindow($w.MainWindowHandle) | Out-Null
        Start-Sleep -Seconds 1
        $r = New-Object Shell+RECT
        [Shell]::GetWindowRect($w.MainWindowHandle, [ref]$r) | Out-Null
        $bmp = New-Object System.Drawing.Bitmap ($r.Right - $r.Left), ($r.Bottom - $r.Top)
        $g = [System.Drawing.Graphics]::FromImage($bmp)
        $g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
        $bmp.Save("$Shots\windows-desktop.png", [System.Drawing.Imaging.ImageFormat]::Png)
        $g.Dispose(); $bmp.Dispose()
    } catch { "desktop: picture not taken: $($_.Exception.Message)" }
}

# 5. The close button hides the window to the notification area: the softphone goes on, with no
# window in sight; a second start brings the window back.
$w = Window
if ($w) {
    $w.CloseMainWindow() | Out-Null
    Start-Sleep -Seconds 3
    $alive = @(Get-Process go-microsip -ErrorAction SilentlyContinue)
    if ($alive.Count -eq 1 -and -not (Window)) {
        'desktop: tray: the close button hid the window and the softphone goes on'
        $again = Run '' 20
        Start-Sleep -Seconds 2
        if ($again.Code -eq 0 -and (Window)) { 'desktop: tray: a second start brought the window back and ended with 0' }
        else { Fail "tray: a second start exit $($again.Code), window in sight: $([bool](Window))" }
    } else { Fail "tray: after the close button $($alive.Count) softphone processes run, window in sight: $([bool](Window))" }
} else { Fail 'tray: no window to close' }

# 5b. The MCP server, started as an agent starts it beside this window: it initializes, lists its
# tools, reaches the window, and is refused what needs a grant. After the steps that send the
# window keys, and before the softphone is ended.
if ($McpTest -and (Test-Path $McpTest)) {
    $env:GO_MICROSIP_MCP_EXE = $Exe
    $log = "$Work\mcp.log"
    $p = Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', "`"`"$McpTest`" -test.v -test.run TestTheMCPServerReachesTheWindowOfThisSession > `"$log`" 2>&1`"" -PassThru -WindowStyle Hidden
    if (-not $p.WaitForExit(90000)) { $p.Kill(); Fail 'mcp: the session test did not end' }
    Get-Content $log -ErrorAction SilentlyContinue | Where-Object { $_ -match 'session_test|^(--- |FAIL|panic)' } | ForEach-Object { "desktop: mcp: $_" }
    if ($p.ExitCode -ne 0) { Fail 'mcp: the session test' }
}

Get-Process go-microsip -ErrorAction SilentlyContinue | Stop-Process -Force

# 6. Notifications: a ringing call is a toast with Answer and Reject. The notifier's own test
# hands the toast to Windows and waits for a button; this script looks for the button on the
# screen and presses it when it finds it. A new Windows installation has its own do-not-disturb
# on for a while, which keeps toasts off the screen: the toast is then in the notification
# center, of which a picture is taken. A button that is not found is reported as not pressed.
if ($NotifyTest) {
    Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
    $env:GO_MICROSIP_NOTIFY_TEST = 'desktop'
    $toast = "$Work\toast.log"
    Remove-Item $toast -ErrorAction SilentlyContinue
    # What earlier runs left in the notification center goes first: what is counted below is
    # this run's toast.
    try {
        [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
        [Windows.UI.Notifications.ToastNotificationManager]::History.Clear('go-microsip')
    } catch { "desktop: notifications: the notification history was not cleared: $($_.Exception.Message)" }
    $t = Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', "`"`"$NotifyTest`" -test.v -test.run TestAToastOnThisDesktop > `"$toast`" 2>&1`"" -PassThru -WindowStyle Hidden
    $pressed = $false
    $pictured = $false
    $opened = $false
    $started = Get-Date
    $deadline = (Get-Date).AddSeconds(30)
    $named = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty, 'Answer')
    while (-not $pressed -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 700
        try {
            $button = [System.Windows.Automation.AutomationElement]::RootElement.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $named)
            # A picture of the screen: when the button is found, or a few seconds after the
            # notification center was opened, whichever comes first.
            if ($Shots -and -not $pictured -and ($button -or ($opened -and ((Get-Date) - $started).TotalSeconds -gt 9))) {
                $screen = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
                $bmp = New-Object System.Drawing.Bitmap $screen.Width, $screen.Height
                $g = [System.Drawing.Graphics]::FromImage($bmp)
                $g.CopyFromScreen(0, 0, 0, 0, $bmp.Size)
                $bmp.Save("$Shots\windows-toast.png", [System.Drawing.Imaging.ImageFormat]::Png)
                $g.Dispose(); $bmp.Dispose()
                $pictured = $true
            }
            if ($button) {
                $button.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
                $pressed = $true
            } elseif (-not $opened -and ((Get-Date) - $started).TotalSeconds -gt 5) {
                Start-Process 'ms-actioncenter:' # the toast is not on the screen: it is in here
                $opened = $true
            }
        } catch { "desktop: notifications: looking for the button: $($_.Exception.Message)" }
    }
    $t.WaitForExit(60000) | Out-Null
    Get-Content $toast -ErrorAction SilentlyContinue | Where-Object { $_ -match 'notify_windows_test|^(--- |FAIL|panic)' } | ForEach-Object { "desktop: notifications: $_" }
    # What Windows holds for go-microsip in its notification center.
    $held = 0
    try {
        [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
        $held = @([Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory('go-microsip')).Count
    } catch { "desktop: notifications: the notification history was not read: $($_.Exception.Message)" }
    if ($t.ExitCode -ne 0) { Fail 'notifications: the toast test' }
    elseif ($pressed -and (Select-String -Path $toast -Pattern 'came back as the decision' -Quiet)) { 'desktop: notifications: the toast was shown and its Answer button, pressed on the screen, decided the call' }
    elseif ($pressed) { Fail 'notifications: a button named Answer was pressed on the screen and no decision came back from the toast' }
    elseif ($held -gt 0) { "desktop: notifications: Windows took the toast and holds $held notification(s) of go-microsip in its notification center; NOT VERIFIED: its buttons (the check did not find one to press, on the screen or in the notification center)" }
    else { Fail 'notifications: Windows holds no notification of go-microsip' }
    [System.Windows.Forms.SendKeys]::SendWait('{ESC}')
}
exit $script:failed
