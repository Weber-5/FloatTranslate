/**
 * Client resolution: inside Tauri use the real fetch client configured by
 * `get_backend_config`; in a pure browser (`npm run dev`) use the mock client.
 * Detection: `'__TAURI_INTERNALS__' in window` (frozen contract).
 */
import type { ApiClient, BackendConfig } from './client'
import { createRealClient, probeBackendHealth } from './client'

export { ApiError, toApiError, createRealClient, probeBackendHealth } from './client'
export { BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE } from './client'
export type {
  ApiClient,
  BackendConfig,
  ChatGenerationHandle,
  ChatGenerationHandlers,
} from './client'

let clientPromise: Promise<ApiClient> | null = null
let resolvedClient: ApiClient | null = null
let resolvedConfig: BackendConfig | null = null
let resolvedIsMock = true

export function isTauri(): boolean {
  return typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window
}

export function isMockMode(): boolean {
  return resolvedIsMock
}

/** Base URL/token of the real backend; null in mock mode or before initApi(). */
export function getBackendConfig(): BackendConfig | null {
  return resolvedConfig
}

function resolveClient(): Promise<ApiClient> {
  if (isTauri()) {
    return import('@tauri-apps/api/core')
      .then(({ invoke }) => invoke<BackendConfig>('get_backend_config'))
      .then((config) => {
        resolvedIsMock = false
        resolvedConfig = config
        return createRealClient(config)
      })
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
  clientPromise = null
  resolvedClient = null
  resolvedConfig = null
  resolvedIsMock = true
}

/** Test-only: inject a backend config so real-mode helpers are testable. */
export function __setBackendConfigForTests(config: BackendConfig | null): void {
  resolvedConfig = config
}

/** Probe the real backend's /health (no auth). Always false in mock mode. */
export async function probeBackend(config?: BackendConfig): Promise<boolean> {
  const target = config ?? resolvedConfig
  if (!target) return false
  return probeBackendHealth(target.base_url)
}
