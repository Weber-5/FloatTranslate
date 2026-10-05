/**
 * Native window actions via Tauri. In pure-browser mock mode these are
 * deliberate no-ops (there is no window to minimize or hide).
 */
import { isTauri } from '@/api'

/** Sidebar panel width in logical px; opening adds it to the window width. */
export const SIDEBAR_WIDTH = 420
const MAIN_WIDTH_KEY = 'ft.main-width'
/** Shrink heuristic at boot: widened beyond this much with the sidebar
 *  closed means the app exited while the sidebar was open. */
const SIDEBAR_RESIDUE_PX = 80

async function safeInvoke(command: string): Promise<void> {
  if (!isTauri()) return
  try {
    const { invoke } = await import('@tauri-apps/api/core')
    await invoke(command)
  } catch {
    // Host command not implemented yet (Phase 1 skeleton) — ignore.
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

interface WindowApis {
  outerSize(): Promise<{ width: number; height: number }>
  scaleFactor(): Promise<number>
  setSize(size: { Logical: { width: number; height: number } } | { Physical: { width: number; height: number } }): Promise<void>
  outerPosition(): Promise<{ x: number; y: number }>
  setPosition(position: { Logical: { x: number; y: number } } | { Physical: { x: number; y: number } }): Promise<void>
  currentMonitor(): Promise<{
    position: { x: number; y: number }
    size: { width: number; height: number }
  } | null>
}

async function windowApis(): Promise<WindowApis | null> {
  if (!isTauri()) return null
  try {
    const mod = await import('@tauri-apps/api/window')
    return mod.getCurrentWindow() as unknown as WindowApis
  } catch {
    return null
  }
}

function readMainWidth(): number | null {
  const raw = window.localStorage.getItem(MAIN_WIDTH_KEY)
  if (raw === null) return null
  const parsed = Number(raw)
  return Number.isFinite(parsed) && parsed >= 360 ? parsed : null
}

function writeMainWidth(width: number): void {
  try {
    window.localStorage.setItem(MAIN_WIDTH_KEY, String(Math.round(width)))
  } catch {
    // Storage unavailable — width persistence is best-effort.
  }
}

async function logicalSize(win: WindowApis): Promise<{ width: number; height: number }> {
  const physical = await win.outerSize()
  const scale = (await win.scaleFactor()) || 1
  return { width: physical.width / scale, height: physical.height / scale }
}

/**
 * Opens the sidebar: remembers the current main width, then widens the
 * window to main + SIDEBAR_WIDTH (height unchanged), shifting the window
 * left when it would overflow the monitor. No-op in mock/browser mode.
 */
export async function expandForSidebar(): Promise<void> {
  const win = await windowApis()
  if (!win) return
  try {
    const size = await logicalSize(win)
    const mainWidth = Math.max(360, size.width - SIDEBAR_WIDTH)
    writeMainWidth(mainWidth)

    const targetWidth = size.width + SIDEBAR_WIDTH
    const monitor = await win.currentMonitor()
    if (monitor) {
      const scale = (await win.scaleFactor()) || 1
      const pos = await win.outerPosition()
      const rightEdge = (monitor.position.x + monitor.size.width) / scale
      const overflow = pos.x + targetWidth - rightEdge
      if (overflow > 0) {
        await win.setPosition({
          Logical: { x: Math.round(pos.x - overflow), y: Math.round(pos.y) },
        })
      }
    }
    await win.setSize({ Logical: { width: Math.round(targetWidth), height: Math.round(size.height) } })
  } catch {
    // Window API failure must not block the sidebar itself.
  }
}

/**
 * Closes the sidebar: restores the remembered main width. No-op in
 * mock/browser mode.
 */
export async function collapseSidebar(): Promise<void> {
  const win = await windowApis()
  if (!win) return
  try {
    const saved = readMainWidth()
    if (saved === null) return
    const size = await logicalSize(win)
    if (size.width <= saved + 2) return // already collapsed
    await win.setSize({ Logical: { width: Math.round(saved), height: Math.round(size.height) } })
  } catch {
    // Best-effort restore.
  }
}

/**
 * Boot-time residue guard: if the app exited while the sidebar was open, the
 * persisted window geometry is the EXPANDED width; with the sidebar closed
 * (it always starts closed) shrink back to the remembered main width.
 */
export async function restoreMainWidthAtBoot(): Promise<void> {
  const win = await windowApis()
  if (!win) return
  try {
    const saved = readMainWidth()
    if (saved === null) return
    const size = await logicalSize(win)
    if (size.width > saved + SIDEBAR_RESIDUE_PX) {
      await win.setSize({ Logical: { width: Math.round(saved), height: Math.round(size.height) } })
    }
  } catch {
    // Best-effort.
  }
}
