/**
 * Native window actions via Tauri. In pure-browser mock mode these are
 * deliberate no-ops (there is no window to minimize or hide).
 *
 * The Tauri modules are imported lazily and typed from the package itself
 * (`typeof import(...)`) so a renamed/removed API is a `vue-tsc` error instead
 * of a silently swallowed runtime TypeError.
 */
import { isTauri } from '@/api'
import type { LogicalPosition, LogicalSize } from '@tauri-apps/api/dpi'
import type { Monitor, Window as TauriWindow } from '@tauri-apps/api/window'

/** Sidebar panel width in logical px; opening adds it to the window width. */
export const SIDEBAR_WIDTH = 420
const MAIN_WIDTH_KEY = 'ft.main-width'
/** Tolerance when recognising the widened (sidebar-open) geometry. */
const SIDEBAR_RESIDUE_PX = 80
/** Frozen minimum main-window width (docs/00 §2). */
const MIN_MAIN_WIDTH = 360

async function safeInvoke(command: string): Promise<void> {
  if (!isTauri()) return
  try {
    const { invoke } = await import('@tauri-apps/api/core')
    await invoke(command)
  } catch {
    // Older host bundle without the command: window actions degrade to no-ops.
  }
}

export function minimizeWindow(): Promise<void> {
  return safeInvoke('minimize_window')
}

/** Close button hides to tray instead of quitting (docs/00 §2). */
export function hideWindowToTray(): Promise<void> {
  return safeInvoke('hide_to_tray')
}

// ---- AI sidebar window expansion (improvement bug #2) -----------------------
//
// The sidebar must fan OUT to the right as an extra panel while the translate
// area keeps its size — not squeeze the content inside a fixed window. The
// window is resized around the saved main width; when the expanded window
// would cross the monitor's right edge it is shifted left first.

interface TauriWindowApi {
  win: TauriWindow
  /** Module-level `currentMonitor` (NOT a Window method — improvement bug #2). */
  currentMonitor: () => Promise<Monitor | null>
  LogicalSize: new (width: number, height: number) => LogicalSize
  LogicalPosition: new (x: number, y: number) => LogicalPosition
}

/** Resolves the Tauri window/dpi handles, or null in pure-browser mode. */
async function tauriWindow(): Promise<TauriWindowApi | null> {
  if (!isTauri()) return null
  try {
    const [windowMod, dpi] = await Promise.all([
      import('@tauri-apps/api/window'),
      import('@tauri-apps/api/dpi'),
    ])
    return {
      win: windowMod.getCurrentWindow(),
      currentMonitor: windowMod.currentMonitor,
      LogicalSize: dpi.LogicalSize,
      LogicalPosition: dpi.LogicalPosition,
    }
  } catch {
    return null
  }
}

/**
 * Current window size in logical pixels.
 *
 * Uses the INNER size because `setSize` writes the inner size: mixing the two
 * made the window grow by the invisible resize border on every open/close
 * cycle.
 */
async function readLogicalSize(
  win: TauriWindow,
): Promise<{ width: number; height: number } | null> {
  try {
    const [inner, scale] = await Promise.all([win.innerSize(), win.scaleFactor()])
    const factor = scale || 1
    return { width: inner.width / factor, height: inner.height / factor }
  } catch {
    return null
  }
}

function readMainWidth(): number | null {
  const raw = window.localStorage.getItem(MAIN_WIDTH_KEY)
  if (raw === null) return null
  const parsed = Number(raw)
  return Number.isFinite(parsed) && parsed >= MIN_MAIN_WIDTH ? parsed : null
}

function writeMainWidth(width: number): void {
  try {
    window.localStorage.setItem(MAIN_WIDTH_KEY, String(Math.round(width)))
  } catch {
    // Storage unavailable — width persistence is best-effort.
  }
}

/**
 * Opens the sidebar: remembers the current main width, then widens the window
 * to main + SIDEBAR_WIDTH (height unchanged), shifting the window left when it
 * would overflow the monitor.
 *
 * The remembered value is the window width BEFORE the sidebar exists — that is
 * exactly the width the translate pane must keep, so closing restores it.
 * Reading geometry, the optional monitor lookup and the resize itself are
 * independent: a missing monitor API must never stop the window from widening
 * (that silent abort was improvement bug #2's real cause).
 */
export async function expandForSidebar(): Promise<void> {
  const tauri = await tauriWindow()
  if (!tauri) return
  const { win } = tauri

  const size = await readLogicalSize(win)
  if (size === null) return

  // Already widened (state desync / repeated call): keep the earlier value so
  // closing still restores the true main width and the window is not widened
  // a second time.
  const saved = readMainWidth()
  const alreadyExpanded = saved !== null && Math.abs(size.width - (saved + SIDEBAR_WIDTH)) <= 2
  const mainWidth = alreadyExpanded ? saved : size.width
  writeMainWidth(mainWidth)

  const targetWidth = Math.round(mainWidth + SIDEBAR_WIDTH)
  const targetHeight = Math.round(size.height)

  // Best-effort: shift left when the widened window would cross the monitor's
  // right edge. All values are converted to logical pixels first.
  try {
    const monitor = await tauri.currentMonitor()
    if (monitor) {
      const factor = monitor.scaleFactor || (await win.scaleFactor()) || 1
      const position = await win.outerPosition()
      const x = position.x / factor
      const rightEdge = (monitor.position.x + monitor.size.width) / factor
      const overflow = x + targetWidth - rightEdge
      if (overflow > 0) {
        await win.setPosition(
          new tauri.LogicalPosition(Math.round(x - overflow), Math.round(position.y / factor)),
        )
      }
    }
  } catch (err) {
    // Monitor lookup/shift is optional; the resize below must still happen.
    console.error('expandForSidebar: monitor shift skipped', err)
  }

  try {
    await win.setSize(new tauri.LogicalSize(targetWidth, targetHeight))
  } catch (err) {
    console.error('expandForSidebar: window resize failed', err)
  }
}

/**
 * Closes the sidebar: restores the remembered main width. No-op in
 * mock/browser mode.
 */
export async function collapseSidebar(): Promise<void> {
  const tauri = await tauriWindow()
  if (!tauri) return
  const saved = readMainWidth()
  if (saved === null) return
  const size = await readLogicalSize(tauri.win)
  if (size === null || size.width <= saved + 2) return // already collapsed
  try {
    await tauri.win.setSize(new tauri.LogicalSize(Math.round(saved), Math.round(size.height)))
  } catch (err) {
    console.error('collapseSidebar: window resize failed', err)
  }
}

/**
 * Boot-time residue guard: if the app exited while the sidebar was open, the
 * persisted window geometry is the EXPANDED width. The sidebar always starts
 * closed, so shrink back — but only when the geometry actually matches the
 * widened width, never when the user simply prefers a wider window.
 */
export async function restoreMainWidthAtBoot(): Promise<void> {
  const tauri = await tauriWindow()
  if (!tauri) return
  const saved = readMainWidth()
  if (saved === null) return
  const size = await readLogicalSize(tauri.win)
  if (size === null) return
  const looksExpanded = Math.abs(size.width - (saved + SIDEBAR_WIDTH)) <= SIDEBAR_RESIDUE_PX
  if (!looksExpanded) return
  try {
    await tauri.win.setSize(new tauri.LogicalSize(Math.round(saved), Math.round(size.height)))
  } catch (err) {
    console.error('restoreMainWidthAtBoot: window resize failed', err)
  }
}
