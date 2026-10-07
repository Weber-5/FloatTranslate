/**
 * Native host commands for Phase 5 hardening (docs/00 §9), one thin wrapper
 * per frozen Tauri contract. The Rust host is the authority; in pure-browser
 * mock mode every command is simulated in-memory so the whole settings UI
 * stays demoable without the desktop shell.
 *
 * Frozen contracts (Phase 5):
 *   set_autostart(enabled)                        → boolean
 *   get_autostart()                               → boolean
 *   set_always_on_top(enabled)                    → void
 *   reset_window_state()                          → void
 *   open_logs_dir()                               → void
 *   open_external_url(url)                        → void
 *   pick_save_path({ options })                   → { ok, path?, cancelled? }
 *   pick_open_path({ options })                   → { ok, path?, cancelled? }
 *
 * Degradation rule: when the host command is missing (older host bundle),
 * wrappers report `unsupported: true` instead of throwing, and the UI shows
 * the "当前环境不支持" notice and keeps the previous value.
 */
import { isTauri } from '@/api'
import { useSettingsStore } from '@/stores/settings'

export interface PickPathOptions {
  default_file_name?: string
  filter_name?: string
  filter_ext?: string
}

export interface PickPathResult {
  ok: boolean
  path?: string
  cancelled?: boolean
  /** Host command missing → the UI shows the unsupported notice. */
  unsupported?: boolean
}

/** Uniform envelope for the value-returning wrappers. */
export interface NativeValue<T> {
  ok: boolean
  value?: T
  unsupported?: boolean
}

type TauriInvoke = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>

let invokeOverride: TauriInvoke | null = null

/**
 * Test-only: replace the Tauri invoke used by every wrapper (simulates the real
 * host). Inert outside dev/test builds so a production bundle cannot have its
 * host calls redirected (UX review 2026-10-07, cleanup decision A).
 */
export function __setNativeInvokeForTests(fn: TauriInvoke | null): void {
  if (!import.meta.env.DEV) return
  invokeOverride = fn
}

async function tauriInvoke(cmd: string, args?: Record<string, unknown>): Promise<unknown> {
  if (invokeOverride) return invokeOverride(cmd, args)
  const { invoke } = await import('@tauri-apps/api/core')
  return invoke(cmd, args)
}

function isRealHost(): boolean {
  return invokeOverride !== null || isTauri()
}

/**
 * Normalizes a host pick-path reply. A rejected invoke (command missing) is
 * reported as unsupported+cancelled so callers can degrade gracefully.
 */
function normalizePickResult(raw: unknown, failed: boolean): PickPathResult {
  if (failed) return { ok: false, cancelled: true, unsupported: true }
  if (raw && typeof raw === 'object') {
    const record = raw as { ok?: unknown; path?: unknown; cancelled?: unknown }
    if (record.cancelled === true) return { ok: false, cancelled: true }
    if (record.ok === true && typeof record.path === 'string' && record.path.length > 0) {
      return { ok: true, path: record.path }
    }
  }
  // Malformed reply: treat like a user cancellation, not a hard failure.
  return { ok: false, cancelled: true }
}

/**
 * Save dialog. Mock mode (no host) falls back to window.prompt with the
 * default file name so the flow stays walkable in `npm run dev`; tests
 * inject fake pickers through __setNativeInvokeForTests instead.
 */
export async function pickSavePath(options: PickPathOptions): Promise<PickPathResult> {
  if (isRealHost()) {
    try {
      const raw = await tauriInvoke('pick_save_path', { options })
      return normalizePickResult(raw, false)
    } catch {
      return normalizePickResult(null, true)
    }
  }
  const suggested = options.default_file_name ?? ''
  const picked = window.prompt('Save backup to:', suggested)
  if (picked === null || picked.trim().length === 0) return { ok: false, cancelled: true }
  return { ok: true, path: picked.trim() }
}

/** Open dialog; same degradation rules as pickSavePath. */
export async function pickOpenPath(options: PickPathOptions): Promise<PickPathResult> {
  if (isRealHost()) {
    try {
      const raw = await tauriInvoke('pick_open_path', { options })
      return normalizePickResult(raw, false)
    } catch {
      return normalizePickResult(null, true)
    }
  }
  const picked = window.prompt('Open backup file:', '')
  if (picked === null || picked.trim().length === 0) return { ok: false, cancelled: true }
  return { ok: true, path: picked.trim() }
}

/**
 * Reads the OS autostart state. `unsupported` (command missing) lets the
 * GeneralSection disable the toggle with a tooltip; mock mode simulates the
 * in-memory state: it mirrors the saved setting so the toggle stays coherent
 * without the desktop shell.
 */
export async function getAutostart(): Promise<NativeValue<boolean>> {
  if (isRealHost()) {
    try {
      const raw = await tauriInvoke('get_autostart')
      if (typeof raw === 'boolean') return { ok: true, value: raw }
      if (raw && typeof raw === 'object') {
        const enabled = (raw as { enabled?: unknown; value?: unknown }).enabled
        if (typeof enabled === 'boolean') return { ok: true, value: enabled }
      }
      return { ok: false, unsupported: true }
    } catch {
      return { ok: false, unsupported: true }
    }
  }
  const settings = useSettingsStore()
  return { ok: true, value: settings.app?.auto_start ?? false }
}

/** Writes the OS autostart registration; resolves with the resulting state. */
export async function setAutostart(enabled: boolean): Promise<NativeValue<boolean>> {
  if (isRealHost()) {
    try {
      const raw = await tauriInvoke('set_autostart', { enabled })
      // Frozen return: boolean; be liberal about wrapper objects.
      if (typeof raw === 'boolean') return { ok: true, value: raw }
      if (raw === undefined || raw === null) return { ok: true, value: enabled }
      if (typeof raw === 'object') {
        const value = (raw as { enabled?: unknown; value?: unknown }).enabled
        if (typeof value === 'boolean') return { ok: true, value }
      }
      return { ok: true, value: enabled }
    } catch {
      return { ok: false, unsupported: true }
    }
  }
  // Mock simulation: the saved setting IS the simulated OS state.
  return { ok: true, value: enabled }
}

export async function setAlwaysOnTop(enabled: boolean): Promise<NativeValue<boolean>> {
  if (isRealHost()) {
    try {
      await tauriInvoke('set_always_on_top', { enabled })
      return { ok: true, value: enabled }
    } catch {
      return { ok: false, unsupported: true }
    }
  }
  return { ok: true, value: enabled }
}

/**
 * Clears the saved window geometry (docs/05 §7: Reset App also resets window
 * state). Pure mock mode: no window state exists, so this succeeds as a no-op;
 * the caller additionally ignores failures (best-effort by contract).
 */
export async function resetWindowState(): Promise<NativeValue<boolean>> {
  if (isRealHost()) {
    try {
      await tauriInvoke('reset_window_state')
      return { ok: true, value: true }
    } catch {
      return { ok: false, unsupported: true }
    }
  }
  return { ok: true, value: true }
}

/**
 * Opens the logs directory. Mock mode: deliberate no-op that reports success
 * so the About row can show its success toast.
 */
export async function openLogsDir(): Promise<NativeValue<boolean>> {
  if (isRealHost()) {
    try {
      await tauriInvoke('open_logs_dir')
      return { ok: true, value: true }
    } catch {
      return { ok: false, unsupported: true }
    }
  }
  return { ok: true, value: true }
}

/**
 * Opens an external `http(s)` URL in the default browser through the host.
 *
 * improvement bug #7: the packaged WebView denies `target="_blank"` popups
 * (Tauri installs no new-window handler, so wry marks the request handled and
 * drops it), which made the "查看更新" / GitHub links dead. The host command
 * re-validates the scheme before handing the URL to the shell.
 *
 * Browser/mock mode falls back to window.open so `npm run dev` keeps working.
 */
export async function openExternalUrl(url: string): Promise<NativeValue<boolean>> {
  if (isRealHost()) {
    try {
      await tauriInvoke('open_external_url', { url })
      return { ok: true, value: true }
    } catch {
      return { ok: false, unsupported: true }
    }
  }
  try {
    window.open(url, '_blank', 'noopener,noreferrer')
  } catch {
    // Pop-up blocked — the caller still shows the URL as selectable text.
  }
  return { ok: true, value: true }
}
