# Run by check.ps1 in the Windows VM's desktop session when the lab PBX is at hand (task 6.3,
# make go-vm GO_VM_LAB=1): the call walkthrough of the window on the exact .exe, driven with the
# keyboard as a person would. Over UDP and over TCP the softphone registers a fictional account
# of the lab, dials the echo extension from the number field, stays in the call and hangs up
# with Escape. Then, as a third account, it is called by the PBX: the call rings, Enter answers
# it and Escape ends it. A picture of the window is taken at each step. The accounts and their
# passwords are the lab's, read from its clients' folder and never printed. Prints one
# "desktop:" line per step; the exit code is the number of steps that failed.
param([Parameter(Mandatory = $true)][string]$Exe, [Parameter(Mandatory = $true)][string]$Work,
    [Parameter(Mandatory = $true)][string]$Pbx, [string]$Lab = 'C:\lab', [string]$Shots = '')
$ErrorActionPreference = 'Stop'
$trace = "$Work\call.jsonl"
. "$PSScriptRoot\window.ps1"
$accounts = Get-Content "$Lab\credentials.json" -Raw | ConvertFrom-Json
# What earlier calls left in the notification center goes first: a toast is not taken away when
# its call is over, and one still coming on screen takes the keyboard from the window.
try {
    [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
    [Windows.UI.Notifications.ToastNotificationManager]::History.Clear('go-microsip')
} catch { "desktop: call: the notification history was not cleared: $($_.Exception.Message)" }

# The softphone, started as one of the lab's accounts.
function Open([string]$User, [string]$Transport) {
    Get-Process go-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 1
    Remove-Item $trace -ErrorAction SilentlyContinue
    $env:GO_MICROSIP_UI_THEME = 'light'
    $env:GO_MICROSIP_ACCOUNT_USERNAME = $User
    $env:GO_MICROSIP_ACCOUNT_DOMAIN = $Pbx
    $env:GO_MICROSIP_ACCOUNT_PORT = '5060'
    $env:GO_MICROSIP_ACCOUNT_TRANSPORT = $Transport
    $env:GO_MICROSIP_SIP_PASSWORD = $accounts.$User
    Start-Process -FilePath $Exe -ArgumentList '--ui-trace', "`"$trace`"" -RedirectStandardError "$Work\call-$User.err" | Out-Null
    Remove-Item Env:GO_MICROSIP_SIP_PASSWORD
}
# At the softphone's first call Windows asks whether its firewall should let the program accept
# connections, and refuses them from that moment: its rules for the program are how this knows
# that it has just asked. The question is a window of Windows' own, on top of the softphone's,
# that desktop automation cannot read and that takes the keyboard when the softphone is brought
# to the front. No call here needs the exception: Escape, its Cancel, is pressed, as a careful
# person would.
function Blocks() {
    return @(Get-NetFirewallRule -DisplayName 'go-microsip' -ErrorAction SilentlyContinue |
        Where-Object { $_.Action -eq 'Block' -and ($_ | Get-NetFirewallApplicationFilter).Program -eq $Exe }).Count
}
$known = Blocks # asked in an earlier run: Windows does not ask twice
function Asked() {
    if ($known -gt 0 -or $script:answered) { return }
    $deadline = (Get-Date).AddSeconds(8)
    while ((Get-Date) -lt $deadline -and (Blocks) -eq 0) { Start-Sleep -Milliseconds 500 }
    if ((Blocks) -eq 0) { return } # Windows did not ask
    $script:answered = $true
    Start-Sleep -Seconds 1
    if ($Shots) { Screen "$Shots\windows-call-firewall-question.png" }
    Fronted | Out-Null
    [System.Windows.Forms.SendKeys]::SendWait('{ESC}')
    Start-Sleep -Seconds 2
    if (Fronted) { "desktop: call: Windows asked whether its firewall should let go-microsip accept connections; Cancel was pressed, and Windows goes on refusing them ($(Blocks) rules)" }
    else { Fail "call: the firewall's question did not go away" }
}
function Shot([string]$Name) { if ($Shots) { Picture "$Shots\windows-call-$Name.png" } }
function Status() { return ((Run 'status --json').Out -replace '\s', '') }

foreach ($case in @(@{ User = '1001'; Transport = 'udp' }, @{ User = '1101'; Transport = 'tcp' })) {
    $what = "$($case.User) over $($case.Transport)"
    Open $case.User $case.Transport
    if (-not (Shows 'Keypad' 90)) { Fail "call: ${what}: the window did not draw its home screen"; continue }
    if (-not (Until '"registration":"registered"' 40)) { Fail "call: ${what}: not registered: $(Status)"; continue }
    "desktop: call: ${what}: the window is open and registered at the lab PBX"
    Shot "$($case.Transport)-1-registered"
    Key '^l'
    Key '600'
    Key '{ENTER}'
    if (-not ((Until '"direction":"outgoing","phase":"established"' 30) -and (Shows 'Connected' 10))) {
        $page = Page
        Fail "call: ${what}: the call to 600 did not connect: $(Status); the page: $($page.Substring(0, [Math]::Min(400, $page.Length)))"
        Shot "$($case.Transport)-failed"
        Get-Content "$Work\call-$($case.User).err" -ErrorAction SilentlyContinue | Where-Object { $_ -match 'WARN|ERROR' } | Select-Object -Last 6 | ForEach-Object { "desktop:   $_" }
        Key '{ESC}'
        continue
    }
    "desktop: call: ${what}: 600 typed in the number field and Enter: the call is connected"
    Asked # the question comes with the first sound of the first call
    Shot "$($case.Transport)-2-connected"
    Start-Sleep -Seconds 8 # sound goes both ways meanwhile; the PBX counts it
    if (-not (Until '"phase":"established"' 5)) { Fail "call: ${what}: the call did not last: $(Status)"; continue }
    Key '{ESC}'
    if (-not (Until '"call":null' 15)) { Fail "call: ${what}: Escape did not end the call: $(Status)"; continue }
    "desktop: call: ${what}: Escape hung up after 8 s in the call"
    Shot "$($case.Transport)-3-ended"
}

# The PBX calls: run.sh has it waiting for this account to register.
$what = 'an incoming call for 1002 over udp'
Open '1002' 'udp'
if (-not ((Shows 'Keypad' 90) -and (Until '"registration":"registered"' 40))) { Fail "call: ${what}: not registered: $(Status)" }
elseif (-not (Until '"direction":"incoming","phase":"ringing"' 60)) { Fail "call: ${what}: nothing rang: $(Status)" }
else {
    "desktop: call: ${what}: it rings in the window"
    Shot 'incoming-1-ringing'
    # What Windows was handed for the ringing call.
    try {
        [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
        $held = @([Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory('go-microsip')).Count
        "desktop: call: ${what}: Windows holds $held notification(s) of go-microsip while it rings"
    } catch { "desktop: call: ${what}: the notification history was not read: $($_.Exception.Message)" }
    Key '{ENTER}' # Answer has the focus
    if (-not (Until '"direction":"incoming","phase":"established"' 20)) { Fail "call: ${what}: Enter did not answer: $(Status)" }
    else {
        "desktop: call: ${what}: Enter answered it"
        Shot 'incoming-2-answered'
        Start-Sleep -Seconds 5
        Key '{ESC}'
        if (Until '"call":null' 15) { "desktop: call: ${what}: Escape hung up" } else { Fail "call: ${what}: Escape did not end the call: $(Status)" }
        # The toast of a call that was answered in the window is taken away.
        Start-Sleep -Seconds 2
        try {
            $left = @([Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory('go-microsip')).Count
            if ($left -eq 0) { "desktop: call: ${what}: its notification is gone from the screen and from the notification center" }
            else { Fail "call: ${what}: Windows still holds $left notification(s) of go-microsip after the call" }
        } catch { "desktop: call: ${what}: the notification history was not read: $($_.Exception.Message)" }
    }
}
Get-Process go-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
exit $script:failed
