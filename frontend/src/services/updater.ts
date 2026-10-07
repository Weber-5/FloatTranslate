/**
 * In-app update (1.1.1): the Tauri updater plugin reads the signed
 * `<release>/latest.json` manifest, verifies the minisign signature and runs the
 * NSIS updater; `restartApp` relaunches afterwards.
 *
 * Every entry point degrades safely outside the desktop host (browser dev,
 * vitest), so the About section can render without the plugin being present.
 */

export interface UpdateProgress {
  /** Bytes downloaded so far. */
  downloaded: number
  /** Total size when the server reported it. */
  total: number | null
  /** 0-100 when the total is known, null otherwise. */
  percent: number | null
}

export type InstallOutcome =
  | { status: 'installed' }
  | { status: 'unavailable' }
  | { status: 'restart-failed'; message: string }
  | { status: 'error'; message: string }

interface PluginUpdate {
  version: string
  downloadAndInstall: (
    onEvent?: (event: {
      event: string
      data?: { contentLength?: number; chunkLength?: number }
    }) => void,
  ) => Promise<void>
}

interface UpdaterPlugin {
  check: () => Promise<PluginUpdate | null>
}

let pluginOverride: UpdaterPlugin | null = null
let relaunchOverride: (() => Promise<void>) | null = null

/** Test-only: inject a fake updater plugin (inert outside dev/test builds). */
export function __setUpdaterForTests(plugin: UpdaterPlugin | null): void {
  if (!import.meta.env.DEV) return
  pluginOverride = plugin
}

/** Test-only: inject a fake relaunch (inert outside dev/test builds). */
export function __setRelaunchForTests(fn: (() => Promise<void>) | null): void {
  if (!import.meta.env.DEV) return
  relaunchOverride = fn
}

/** True when the app runs inside the desktop host. */
function hasHost(): boolean {
  return typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window
}

/**
 * Whether an update can actually be downloaded and installed here: the desktop
 * host provides the updater plugin (dev/test builds may inject one). The About
 * section uses this to decide between 「立即更新」 and the manual download link.
 */
export function isUpdaterAvailable(): boolean {
  return pluginOverride !== null || hasHost()
}

async function loadPlugin(): Promise<UpdaterPlugin | null> {
  if (pluginOverride) return pluginOverride
  if (!hasHost()) return null
  try {
    const mod = await import('@tauri-apps/plugin-updater')
    return { check: () => mod.check() as Promise<PluginUpdate | null> }
  } catch {
    return null
  }
}

/**
 * Downloads and installs the pending update, reporting progress. Resolves with
 * the outcome instead of throwing so the caller can show a precise message
 * (a published release without a signed updater bundle is 'unavailable').
 */
export async function installUpdateNow(
  onProgress: (progress: UpdateProgress) => void,
): Promise<InstallOutcome> {
  const plugin = await loadPlugin()
  if (!plugin) return { status: 'unavailable' }
  try {
    const update = await plugin.check()
    if (!update) return { status: 'unavailable' }

    let downloaded = 0
    let total: number | null = null
    await update.downloadAndInstall((event) => {
      if (event.event === 'Started') {
        total = event.data?.contentLength ?? null
      } else if (event.event === 'Progress') {
        downloaded += event.data?.chunkLength ?? 0
      } else if (event.event === 'Finished') {
        downloaded = total ?? downloaded
      }
      onProgress({
        downloaded,
        total,
        percent: total && total > 0 ? Math.min(100, Math.round((downloaded / total) * 100)) : null,
      })
    })
    return { status: 'installed' }
  } catch (error) {
    return { status: 'error', message: error instanceof Error ? error.message : String(error) }
  }
}

/** Relaunches the app so the freshly installed version takes over. */
export async function restartApp(): Promise<InstallOutcome> {
  try {
    if (relaunchOverride) {
      await relaunchOverride()
      return { status: 'installed' }
    }
    if (!hasHost()) return { status: 'restart-failed', message: 'no desktop host' }
    const { relaunch } = await import('@tauri-apps/plugin-process')
    await relaunch()
    return { status: 'installed' }
  } catch (error) {
    return { status: 'restart-failed', message: error instanceof Error ? error.message : String(error) }
  }
}
