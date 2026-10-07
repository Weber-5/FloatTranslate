/**
 * Client resolution: inside Tauri use the real fetch client configured by
 * `get_backend_config`; in a pure browser (`npm run dev`) use the mock client.
 * Detection: `'__TAURI_INTERNALS__' in window` (frozen contract).
 */
import type { ApiClient, BackendConfig } from './client'
import { createRealClient, probeBackendHealth } from './client'

export { ApiError, toApiError, createRealClient, probeBackendHealth } from './client'
export { BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE } from './client'
export { createSseParser } from './sse'
export type { SseEvent, SseParser, SseDataEnvelope } from './sse'
export type {
  ApiClient,
  BackendConfig,
  ChatGenerationCompletedData,
  ChatGenerationHandle,
  ChatGenerationHandlers,
  ChatGenerationStartedInfo,
  ChatStreamOutcome,
  StreamOptions,
} from './client'

let clientPromise: Promise<ApiClient> | null = null
let resolvedClient: ApiClient | null = null
let resolvedConfig: BackendConfig | null = null
let resolvedIsMock = true

const CONFIG_RETRY_FIRST_MS = 250
const CONFIG_RETRY_MAX_MS = 2000

export function isTauri(): boolean {
  return typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window
}

/**
 * Mock/real decision. Before `initApi()` finishes (the real path retries
 * `get_backend_config` until the sidecar is READY), fall back to the
 * synchronous Tauri detection so the backend gate can render immediately.
 */
export function isMockMode(): boolean {
  if (clientPromise === null || resolvedClient === null) return !isTauri()
  return resolvedIsMock
}

/** Base URL/token of the real backend; null in mock mode or before initApi(). */
export function getBackendConfig(): BackendConfig | null {
  return resolvedConfig
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function resolveRealClientWithRetry(): Promise<ApiClient> {
  const { invoke } = await import('@tauri-apps/api/core')
  let delay = CONFIG_RETRY_FIRST_MS
  for (;;) {
    try {
      const config = await invoke<BackendConfig>('get_backend_config')
      // `reconnectBackend()` may have won the race while we were waiting.
      if (resolvedClient) return resolvedClient
      resolvedIsMock = false
      resolvedConfig = config
      const client = createRealClient(config)
      resolvedClient = client
      return client
    } catch {
      // Sidecar not READY yet (docs/01 §3 `starting`): keep the gate up and
      // poll. The host restarts the sidecar with backoff; never give up so a
      // slow first boot can never brick the app into a blank screen.
      await sleep(delay)
      delay = Math.min(delay * 2, CONFIG_RETRY_MAX_MS)
    }
  }
}

function resolveClient(): Promise<ApiClient> {
  if (isTauri()) {
    return resolveRealClientWithRetry()
  }
  // Browser-only development path (`npm run dev`, browser tests). The
  // import.meta.env.DEV guard is what keeps the mock client and its fake word
  // bank OUT of production bundles: the shipped desktop app must never contain
  // simulated model data (UX review 2026-10-07, cleanup decision A). Inside the
  // Tauri host this branch is unreachable — isTauri() short-circuits above.
  if (!import.meta.env.DEV) {
    return Promise.reject(
      new Error('No API client: FloatTranslate must run inside its desktop host.'),
    )
  }
  return import('./mock').then(({ createMockClient }) => {
    resolvedIsMock = true
    resolvedConfig = null
    return createMockClient()
  })
}

/** Initialize the API client once. Called during app bootstrap. */
export function initApi(): Promise<ApiClient> {
  if (!clientPromise) {
    clientPromise = resolveClient().then((client) => {
      resolvedClient = client
      return client
    })
  }
  return clientPromise
}

/** Async accessor — always safe. */
export async function getClient(): Promise<ApiClient> {
  return initApi()
}

/** Synchronous accessor for stores/components; requires initApi() first. */
export function useApi(): ApiClient {
  if (!resolvedClient) {
    throw new Error('API client not initialized — call initApi() during app bootstrap first.')
  }
  return resolvedClient
}

/** Test-only: reset cached resolution so a fresh client can be created. */
export function __resetApiClient(): void {
  if (!import.meta.env.DEV) return
  clientPromise = null
  resolvedClient = null
  resolvedConfig = null
  resolvedIsMock = true
}

/** Test-only: inject a backend config so real-mode helpers are testable. */
export function __setBackendConfigForTests(config: BackendConfig | null): void {
  if (!import.meta.env.DEV) return
  resolvedConfig = config
}

/** Probe the real backend's /health (no auth). Always false in mock mode. */
export async function probeBackend(config?: BackendConfig): Promise<boolean> {
  const target = config ?? resolvedConfig
  if (!target) return false
  return probeBackendHealth(target.base_url)
}

/**
 * Re-resolves `get_backend_config` and rebuilds the real client.
 *
 * Used when the host reports the sidecar became READY again: restarts bind a
 * NEW ephemeral port, so the cached base URL must be refreshed before the
 * next probe (docs/02 §6: runtime connection state is re-established).
 * No-op in mock mode; rejects while the sidecar is still not READY.
 */
export async function reconnectBackend(): Promise<void> {
  if (!isTauri()) return
  const { invoke } = await import('@tauri-apps/api/core')
  const config = await invoke<BackendConfig>('get_backend_config')
  resolvedIsMock = false
  resolvedConfig = config
  const client = createRealClient(config)
  resolvedClient = client
  clientPromise = Promise.resolve(client)
}
