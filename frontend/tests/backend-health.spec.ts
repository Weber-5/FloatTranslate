import { describe, expect, it, vi, afterEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { initApi, __setBackendConfigForTests, type BackendConfig } from '@/api'
import { useBackendStore } from '@/stores/backend'

const config: BackendConfig = {
  base_url: 'http://127.0.0.1:8123',
  token: 'session-token-1',
  data_root: 'C:\\data',
}

afterEach(() => {
  __setBackendConfigForTests(null)
  vi.unstubAllGlobals()
})

describe('backend health store (docs/01 §3: starting → ready → restarting → ready | failed)', () => {
  it('marks ready when the /health probe succeeds', async () => {
    setActivePinia(createPinia())
    await initApi()
    __setBackendConfigForTests(config)
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200 })))

    const backend = useBackendStore()
    backend.markStarting()
    await expect(backend.probe()).resolves.toBe(true)
    expect(backend.status).toBe('ready')
    expect(backend.ready).toBe(true)
  })

  it('marks failed when the probe fails and the retry path shows restarting first', async () => {
    setActivePinia(createPinia())
    await initApi()
    __setBackendConfigForTests(config)
    vi.stubGlobal('fetch', vi.fn(async () => {
      throw new TypeError('fetch failed')
    }))

    const backend = useBackendStore()
    // Simulate the real-mode boot: starting until the first probe resolves.
    backend.markStarting()
    await expect(backend.probe(config)).resolves.toBe(false)
    expect(backend.status).toBe('failed')

    // Retry keeps failing until the backend comes back.
    await expect(backend.retry(config)).resolves.toBe(false)
    expect(backend.status).toBe('failed')

    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 200 })))
    await expect(backend.retry(config)).resolves.toBe(true)
    expect(backend.status).toBe('ready')
  })

  it('initial probe succeeds immediately in mock mode (no config needed)', async () => {
    setActivePinia(createPinia())
    await initApi()
    const backend = useBackendStore()
    await expect(backend.initialProbe()).resolves.toBe(true)
    expect(backend.ready).toBe(true)
  })

  it('consumes host events: backend-status / backend-failed', async () => {
    setActivePinia(createPinia())
    await initApi()
    const backend = useBackendStore()

    backend.applyHostEvent('backend-status', { status: 'starting' })
    expect(backend.status).toBe('starting')

    backend.applyHostEvent('backend-status', 'ready')
    expect(backend.status).toBe('ready')

    backend.applyHostEvent('backend-status', { status: 'restarting' })
    expect(backend.status).toBe('restarting')

    backend.applyHostEvent('backend-failed', null)
    expect(backend.status).toBe('failed')

    // Unknown payloads are ignored (status unchanged).
    backend.applyHostEvent('backend-status', { something: 'else' })
    expect(backend.status).toBe('failed')
  })
})
