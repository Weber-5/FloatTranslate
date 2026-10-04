/**
 * Real-mode boot regression tests (white-screen fix):
 * the webview usually boots before the sidecar is READY, so `initApi` must
 * retry `get_backend_config` (never reject), mock/real must be decidable
 * BEFORE resolution (the gate renders immediately), and `reconnectBackend`
 * must refresh the cached config when a restarted sidecar rebinds its port.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import {
  initApi,
  reconnectBackend,
  isMockMode,
  getBackendConfig,
  __resetApiClient,
  type BackendConfig,
} from '@/api'

const coreMock = vi.hoisted(() => ({ invoke: vi.fn() }))
vi.mock('@tauri-apps/api/core', () => coreMock)

const config = (port: number): BackendConfig => ({
  base_url: `http://127.0.0.1:${port}/api/v1`,
  token: 'session-token',
  data_root: 'C:\\data',
})

function enterTauri(): void {
  ;(window as unknown as Record<string, unknown>).__TAURI_INTERNALS__ = {}
}

beforeEach(() => {
  __resetApiClient()
  coreMock.invoke.mockReset()
  enterTauri()
})

afterEach(() => {
  __resetApiClient()
  delete (window as unknown as Record<string, unknown>).__TAURI_INTERNALS__
})

describe('real-mode boot (white-screen regression)', () => {
  it('presumes real mode before initApi resolves so the gate can render', () => {
    coreMock.invoke.mockImplementation(() => new Promise(() => undefined)) // pending
    void initApi()
    expect(isMockMode()).toBe(false)
  })

  it('retries get_backend_config until the sidecar is READY and never rejects', async () => {
    let calls = 0
    coreMock.invoke.mockImplementation(async () => {
      calls += 1
      if (calls < 3) throw new Error('backend not ready: starting')
      return config(9001)
    })

    await expect(initApi()).resolves.toBeTruthy()
    expect(calls).toBe(3)
    expect(isMockMode()).toBe(false)
    expect(getBackendConfig()?.base_url).toBe('http://127.0.0.1:9001/api/v1')
  })

  it('reconnectBackend refreshes the cached config for restarted sidecars', async () => {
    coreMock.invoke.mockResolvedValueOnce(config(9001))
    await initApi()
    expect(getBackendConfig()?.base_url).toBe('http://127.0.0.1:9001/api/v1')

    coreMock.invoke.mockResolvedValueOnce(config(9102))
    await expect(reconnectBackend()).resolves.toBeUndefined()
    expect(getBackendConfig()?.base_url).toBe('http://127.0.0.1:9102/api/v1')
  })

  it('reconnectBackend rejects while the sidecar is still not READY', async () => {
    coreMock.invoke.mockResolvedValueOnce(config(9001))
    await initApi()
    coreMock.invoke.mockRejectedValueOnce(new Error('backend not ready: starting'))
    await expect(reconnectBackend()).rejects.toThrow('backend not ready')
  })
})
