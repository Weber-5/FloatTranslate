/**
 * Global hotkey registration (docs/07 §4). Rust is the registration
 * authority: the frontend only forwards the current pair via the Tauri
 * command `apply_hotkeys` and reports the result. Frozen contract:
 *   apply_hotkeys({ show_hide, translate_selection })
 *     → Promise<{ registered: boolean, conflict: string | null }>
 *
 * In mock mode (pure browser dev) the registration is simulated: it always
 * succeeds unless one hotkey equals the other one (a self-conflict).
 */
import { initApi, isMockMode, isTauri } from '@/api'
import { DEFAULT_HOTKEY_QUICK_TRANSLATE, DEFAULT_HOTKEY_TOGGLE } from '@/constants'
import { useSettingsStore } from '@/stores/settings'

export interface HotkeyPair {
  show_hide: string
  translate_selection: string
}

export interface HotkeyApplyResult {
  registered: boolean
  conflict: string | null
}

/** Canonical modifier + key vocabulary of the hotkey string format. */
export const HOTKEY_MODIFIERS = ['Ctrl', 'Alt', 'Shift', 'Win'] as const

type TauriInvoke = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>

let invokeOverride: TauriInvoke | null = null

/** Test-only: replace the Tauri invoke used for apply_hotkeys. */
export function __setHotkeyInvokeForTests(fn: TauriInvoke | null): void {
  invokeOverride = fn
}

async function tauriInvoke(cmd: string, args?: Record<string, unknown>): Promise<unknown> {
  if (invokeOverride) return invokeOverride(cmd, args)
  const { invoke } = await import('@tauri-apps/api/core')
  return invoke(cmd, args)
}

/**
 * Minimal client-side format check (docs/07 §4: 先验证格式): at least one
 * leading modifier and one key, e.g. "Ctrl+Alt+Q".
 */
export function isValidHotkeyFormat(value: string): boolean {
  const parts = value
    .split('+')
    .map((part) => part.trim())
    .filter((part) => part.length > 0)
  if (parts.length < 2) return false
  const key = parts[parts.length - 1]
  const modifiers = parts.slice(0, -1)
  if (HOTKEY_MODIFIERS.includes(key as (typeof HOTKEY_MODIFIERS)[number])) return false
  return modifiers.every((part) => HOTKEY_MODIFIERS.includes(part as (typeof HOTKEY_MODIFIERS)[number]))
}

export async function applyHotkeys(hotkeys: HotkeyPair): Promise<HotkeyApplyResult> {
  // invokeOverride is test-only; production requires the Tauri host.
  if (invokeOverride || (isTauri() && !isMockMode())) {
    try {
      const result = (await tauriInvoke('apply_hotkeys', { hotkeys })) as {
        registered?: unknown
        conflict?: unknown
      } | null
      if (result && typeof result === 'object') {
        return {
          registered: result.registered === true,
          conflict: typeof result.conflict === 'string' && result.conflict.length > 0 ? result.conflict : null,
        }
      }
      return { registered: false, conflict: null }
    } catch {
      // Host command failed/missing: keep the previous registration active by
      // reporting a conflict so the UI does not persist the new binding.
      return { registered: false, conflict: null }
    }
  }
  // Mock simulation: always registered unless the pair collides with itself.
  if (hotkeys.show_hide === hotkeys.translate_selection) {
    return { registered: false, conflict: hotkeys.show_hide }
  }
  return { registered: true, conflict: null }
}

/**
 * Real-mode boot step (frozen sequence): after health is ready and settings
 * are loaded, register the hotkeys stored in the Go settings.
 *
 * improvement bug #5: the host can report READY *before* the API client is
 * resolved (the already-running sidecar wins the race against the webview), and
 * `backend.ready` then flips synchronously. Loading settings at that moment
 * threw "API client not initialized", left `settings.app` null and returned
 * silently — so on a fresh install the boot hotkeys were NEVER registered and
 * `hotkeys.json` was never written. Waiting for the client first removes the
 * race: whether the READY event or `initApi()` wins no longer matters.
 */
export async function registerHotkeysFromSettings(): Promise<HotkeyApplyResult | null> {
  try {
    await initApi()
  } catch {
    return null
  }
  const settings = useSettingsStore()
  if (settings.status !== 'success') {
    await settings.load()
  }
  const app = settings.app
  if (!app) {
    // Visible on purpose: a silent null here is what hid the bug above.
    console.error('registerHotkeysFromSettings: app settings unavailable')
    return null
  }
  return applyHotkeys({
    show_hide: app.hotkey_toggle_window ?? DEFAULT_HOTKEY_TOGGLE,
    translate_selection: app.hotkey_quick_translate ?? DEFAULT_HOTKEY_QUICK_TRANSLATE,
  })
}
