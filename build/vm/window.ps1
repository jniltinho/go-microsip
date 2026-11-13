# The softphone's window as a person at the desk sees and uses it, for the scripts that
# check.ps1 runs in the Windows VM's desktop session (desktop.ps1, call.ps1). Dot-sourced: the
# caller has set $Exe (the softphone), $Work (a directory for what it leaves) and $trace (the
# file the window reports what it shows to, with --ui-trace).
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Shell {
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll")] public static extern bool AttachThreadInput(uint from, uint to, bool attach);
    [DllImport("kernel32.dll")] public static extern uint GetCurrentThreadId();
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
# The window in front, with the keyboard. Windows refuses the front to a program that was not the
# last to get input (another window, a notification coming on screen): then this script's input
# is joined to that of whatever is in front, which is what lets a program hand the front over.
function Fronted() {
    $w = Window
    if (-not $w) { return $false }
    $h = $w.MainWindowHandle
    foreach ($try in 1..6) {
        [Shell]::ShowWindow($h, 9) | Out-Null
        [Shell]::SetForegroundWindow($h) | Out-Null
        Start-Sleep -Milliseconds 300
        # In front is a window of the softphone, whichever: the page keeps the keyboard it has.
        $owner = 0
        $thread = [Shell]::GetWindowThreadProcessId([Shell]::GetForegroundWindow(), [ref]$owner)
        if ($owner -eq $w.Id) { return $true }
        $me = [Shell]::GetCurrentThreadId()
        [Shell]::AttachThreadInput($me, $thread, $true) | Out-Null
        [Shell]::SetForegroundWindow($h) | Out-Null
        [Shell]::AttachThreadInput($me, $thread, $false) | Out-Null
        Start-Sleep -Milliseconds 300
    }
    $owner = 0
    [Shell]::GetWindowThreadProcessId([Shell]::GetForegroundWindow(), [ref]$owner) | Out-Null
    return ($owner -eq $w.Id)
}
function Front() {
    if (Fronted) { return }
    $owner = 0
    [Shell]::GetWindowThreadProcessId([Shell]::GetForegroundWindow(), [ref]$owner) | Out-Null
    "desktop: the window did not come to the front; in front: a window of $((Get-Process -Id $owner -ErrorAction SilentlyContinue).ProcessName)"
}
# A picture of the whole screen.
function Screen([string]$File) {
    try {
        $all = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
        $bmp = New-Object System.Drawing.Bitmap $all.Width, $all.Height
        $g = [System.Drawing.Graphics]::FromImage($bmp)
        $g.CopyFromScreen(0, 0, 0, 0, $bmp.Size)
        $bmp.Save($File, [System.Drawing.Imaging.ImageFormat]::Png)
        $g.Dispose(); $bmp.Dispose()
    } catch { "desktop: picture not taken: $($_.Exception.Message)" }
}
# A key for the window, as a person at the keyboard.
# Nothing is typed into a window that is not the softphone's: that is a failed step.
function Key([string]$Keys) {
    if (-not (Fronted)) {
        Front
        Fail "the key $Keys was not sent: the softphone's window is not in front"
        return
    }
    Start-Sleep -Milliseconds 200
    [System.Windows.Forms.SendKeys]::SendWait($Keys)
}
# A picture of the window, for the receipt.
function Picture([string]$File) {
    try {
        Front
        $w = Window
        Start-Sleep -Milliseconds 400
        $r = New-Object Shell+RECT
        [Shell]::GetWindowRect($w.MainWindowHandle, [ref]$r) | Out-Null
        $bmp = New-Object System.Drawing.Bitmap ($r.Right - $r.Left), ($r.Bottom - $r.Top)
        $g = [System.Drawing.Graphics]::FromImage($bmp)
        $g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
        $bmp.Save($File, [System.Drawing.Imaging.ImageFormat]::Png)
        $g.Dispose(); $bmp.Dispose()
    } catch { "desktop: picture not taken: $($_.Exception.Message)" }
}
# The softphone's status, asked again until it matches or the time is over.
function Until([string]$Pattern, [int]$Seconds) {
    $deadline = (Get-Date).AddSeconds($Seconds)
    while ((Get-Date) -lt $deadline) {
        if ((Run 'status --json').Out -match $Pattern) { return $true }
        Start-Sleep -Milliseconds 700
    }
    return $false
}
