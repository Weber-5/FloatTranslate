param(
  [string]$ProcessName = 'FloatTranslate*',
  [string]$OutPath = 'E:\wwb\Translater\dist\e2e-window.png'
)
Add-Type @'
using System;
using System.Runtime.InteropServices;
public class FTWin32 {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  public struct RECT { public int Left, Top, Right, Bottom; }
}
'@
Add-Type -AssemblyName System.Drawing
$p = Get-Process | Where-Object { $_.ProcessName -like $ProcessName } | Select-Object -First 1
if (-not $p) { throw "no process like $ProcessName found" }
$h = $p.MainWindowHandle
[FTWin32]::SetForegroundWindow($h) | Out-Null
Start-Sleep -Milliseconds 800
$r = New-Object FTWin32+RECT
[FTWin32]::GetWindowRect($h, [ref]$r) | Out-Null
$w = $r.Right - $r.Left
$ht = $r.Bottom - $r.Top
$bmp = New-Object System.Drawing.Bitmap($w, $ht)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($r.Left, $r.Top, 0, 0, $bmp.Size)
$bmp.Save($OutPath, [System.Drawing.Imaging.ImageFormat]::Png)
Write-Host ("captured {0}x{1} -> {2}" -f $w, $ht, $OutPath)
