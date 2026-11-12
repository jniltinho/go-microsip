# Run by check.ps1 in the Windows VM's desktop session: the window is started once in each skin
# (no account: nothing registers), and its own report of what it shows (--ui-trace) must name that
# skin, the home screen and the skin's bundled font as loaded. A picture of the desktop is kept for
# each skin. Prints one "skin:" line per skin; the exit code is the number of skins not drawn.
param([Parameter(Mandatory = $true)][string]$Exe, [Parameter(Mandatory = $true)][string]$Work, [string]$Shots = '')
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
# The bundled family each skin draws with (frontend/src/fonts/README.md); Windows has no Ubuntu font.
$fonts = @{ light = 'Inter'; dark = 'Inter'; xp = 'DejaVu Sans'; macos = 'Inter'; ubuntu = 'Inter'; win98 = 'Liberation Sans'; matrix = 'JetBrains Mono' }
$failed = 0
foreach ($skin in 'light', 'dark', 'xp', 'macos', 'ubuntu', 'win98', 'matrix') {
    $trace = "$Work\skin-$skin.jsonl"
    Remove-Item $trace -ErrorAction SilentlyContinue
    $env:GO_MICROSIP_UI_THEME = $skin
    Start-Process -FilePath $Exe -ArgumentList '--ui-trace', "`"$trace`"" | Out-Null
    $drawn = $false
    $deadline = (Get-Date).AddSeconds(90)
    while (-not $drawn -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 500
        foreach ($line in @(Get-Content $trace -ErrorAction SilentlyContinue)) {
            if ($line -match '"page"' -and $line -match "`"theme`":`"$skin`"" -and
                $line -match "`"fonts`":\[[^\]]*`"$($fonts[$skin])`"" -and $line -match 'Keypad') {
                $drawn = $true
                break
            }
        }
    }
    if ($drawn -and $Shots) {
        Start-Sleep -Seconds 1
        $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
        $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
        $g = [System.Drawing.Graphics]::FromImage($bmp)
        $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
        $bmp.Save("$Shots\windows-skin-$skin.png", [System.Drawing.Imaging.ImageFormat]::Png)
        $g.Dispose()
        $bmp.Dispose()
    }
    Get-Process go-microsip -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Milliseconds 800
    if ($drawn) { "skin: $skin drawn on WebView2 in $($fonts[$skin])" }
    else { "skin: $skin NOT drawn"; $failed++ }
}
exit $failed
