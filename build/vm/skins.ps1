# Run by check.ps1 in the Windows VM's desktop session: the window is started once in each skin
# (no account: nothing registers), and its own report of what it shows (--ui-trace) must name that
# skin, the home screen, every face of the skin's bundled font as loaded and none as failed. A picture of the window is kept for
# each skin. Prints one "skin:" line per skin; the exit code is the number of skins not drawn.
param([Parameter(Mandatory = $true)][string]$Exe, [Parameter(Mandatory = $true)][string]$Work, [string]$Shots = '')
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class Desk {
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
}
"@
# The bundled faces each skin draws its home screen with, as "family weight"
# (frontend/src/fonts/README.md); Windows has no Ubuntu font.
$inter = @('Inter 100 900')
$fonts = @{ light = $inter; dark = $inter; macos = $inter; ubuntu = $inter
    xp = @('DejaVu Sans 400', 'DejaVu Sans 700'); win98 = @('Liberation Sans 400', 'Liberation Sans 700')
    matrix = @('JetBrains Mono 400', 'JetBrains Mono 600') }
$failed = 0
foreach ($skin in 'light', 'dark', 'xp', 'macos', 'ubuntu', 'win98', 'matrix') {
    $trace = "$Work\skin-$skin.jsonl"
    Remove-Item $trace -ErrorAction SilentlyContinue
    $env:GO_MICROSIP_UI_THEME = $skin
    $err = "$Work\skin-$skin.err"
    Remove-Item $err -ErrorAction SilentlyContinue
    Start-Process -FilePath $Exe -ArgumentList '--ui-trace', "`"$trace`"" -RedirectStandardError $err | Out-Null
    $drawn = $false
    $deadline = (Get-Date).AddSeconds(90)
    while (-not $drawn -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 500
        foreach ($line in @(Get-Content $trace -ErrorAction SilentlyContinue)) {
            # The home screen with its storage: settings, contacts and history need the database.
            if ($line -match '"page"' -and $line -match "`"theme`":`"$skin`"" -and $line -match '"fontErrors":\[\]' -and $line -match 'Keypad' -and $line -notmatch 'database is not available') {
                $all = $true
                foreach ($face in $fonts[$skin]) { if ($line -notmatch "`"fonts`":\[[^\]]*`"$face`"") { $all = $false } }
                if ($all) { $drawn = $true; break }
            }
        }
    }
    if ($drawn -and $Shots) {
        # A picture of the window alone, in front of whatever the desktop shows (a fresh logon
        # leaves the Start menu open). A picture that cannot be taken does not fail the skin.
        try {
            [System.Windows.Forms.SendKeys]::SendWait('{ESC}')
            $w = Get-Process go-microsip -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
            [Desk]::ShowWindow($w.MainWindowHandle, 9) | Out-Null
            [Desk]::SetForegroundWindow($w.MainWindowHandle) | Out-Null
            Start-Sleep -Seconds 1
            $r = New-Object Desk+RECT
            [Desk]::GetWindowRect($w.MainWindowHandle, [ref]$r) | Out-Null
            $bmp = New-Object System.Drawing.Bitmap ($r.Right - $r.Left), ($r.Bottom - $r.Top)
            $g = [System.Drawing.Graphics]::FromImage($bmp)
            $g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
            $bmp.Save("$Shots\windows-skin-$skin.png", [System.Drawing.Imaging.ImageFormat]::Png)
            $g.Dispose()
            $bmp.Dispose()
        } catch { "skin: $skin picture not taken: $($_.Exception.Message)" }
    }
    Get-Process go-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Milliseconds 800
    if ($drawn) { "skin: $skin drawn on WebView2 in $($fonts[$skin] -join ', '), no font failed" }
    else {
        "skin: $skin NOT drawn"
        $failed++
        # What it did show and say, to tell why: the last report of the page and the last lines of
        # its standard error.
        $last = @(Get-Content $trace -ErrorAction SilentlyContinue | Where-Object { $_ -match '"page"' }) | Select-Object -Last 1
        if ($last) { "skin: $skin last report: " + $last.Substring(0, [Math]::Min(400, $last.Length)) } else { "skin: $skin made no report" }
        Get-Content $err -Tail 5 -ErrorAction SilentlyContinue | ForEach-Object { "skin: $skin stderr: $_" }
    }
}
exit $failed
