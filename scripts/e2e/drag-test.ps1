param(
  [string]$OutPath = 'E:\wwb\Translater\dist\e2e-window.png',
  [int]$GrabOffsetX = 120,
  [int]$GrabOffsetY = 22,
  [int]$DragDx = 130,
  [int]$DragDy = 80
)
Add-Type @'
using System;
using System.Runtime.InteropServices;
public class FTWin32 {
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int X, int Y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint dwFlags, uint dx, uint dy, uint dwData, UIntPtr dwExtraInfo);
  public const uint LEFTDOWN = 0x0002;
  public const uint LEFTUP = 0x0004;
  public struct RECT { public int Left, Top, Right, Bottom; }
}
'@
Add-Type -AssemblyName System.Drawing
$p = Get-Process floattranslate -ErrorAction Stop
$h = $p.MainWindowHandle

function Get-Rect {
  $r = New-Object FTWin32+RECT
  [FTWin32]::GetWindowRect($h, [ref]$r) | Out-Null
  return $r
}

[FTWin32]::SetForegroundWindow($h) | Out-Null
Start-Sleep -Milliseconds 500

$before = Get-Rect
Write-Host ("before: {0},{1} size {2}x{3}" -f $before.Left, $before.Top, ($before.Right - $before.Left), ($before.Bottom - $before.Top))

# Grab INSIDE the client area (past the 8px invisible resize borders), on the
# empty tab-bar background (fresh profile: no tabs; the "+" button sits at the
# far left of the bar).
$grabX = $before.Left + $GrabOffsetX
$grabY = $before.Top + $GrabOffsetY
[FTWin32]::SetCursorPos($grabX, $grabY) | Out-Null
Start-Sleep -Milliseconds 250
[FTWin32]::mouse_event([FTWin32]::LEFTDOWN, 0, 0, 0, [UIntPtr]::Zero)
Start-Sleep -Milliseconds 200

$steps = 14
for ($i = 1; $i -le $steps; $i++) {
  $x = $grabX + [int]($DragDx * $i / $steps)
  $y = $grabY + [int]($DragDy * $i / $steps)
  [FTWin32]::SetCursorPos($x, $y) | Out-Null
  Start-Sleep -Milliseconds 40
}
Start-Sleep -Milliseconds 250
[FTWin32]::mouse_event([FTWin32]::LEFTUP, 0, 0, 0, [UIntPtr]::Zero)
Start-Sleep -Milliseconds 400

$after = Get-Rect
$dx = $after.Left - $before.Left
$dy = $after.Top - $before.Top
$dw = ($after.Right - $after.Left) - ($before.Right - $before.Left)
Write-Host ("after: {0},{1}  moved: {2},{3}  widthDelta: {4}" -f $after.Left, $after.Top, $dx, $dy, $dw)

$r2 = $after
$w = $r2.Right - $r2.Left
$ht = $r2.Bottom - $r2.Top
$bmp = New-Object System.Drawing.Bitmap($w, $ht)
$g = [System.Drawing.Graphics]::FromImage($bmp)
$g.CopyFromScreen($r2.Left, $r2.Top, 0, 0, $bmp.Size)
$bmp.Save($OutPath, [System.Drawing.Imaging.ImageFormat]::Png)

if ([Math]::Abs($dx) -lt 60 -or [Math]::Abs($dy) -lt 40 -or [Math]::Abs($dw) -gt 20) {
  Write-Error ("drag verification failed: moved ${dx},${dy} resized ${dw}")
  exit 1
}
Write-Host DRAG-OK
