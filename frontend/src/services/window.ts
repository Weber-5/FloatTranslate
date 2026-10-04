/**
 * Native window actions via Tauri. In pure-browser mock mode these are
 * deliberate no-ops (there is no window to minimize or hide).
 */
import { isTauri } from '@/api'

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
