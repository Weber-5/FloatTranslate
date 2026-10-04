/**
 * Backend health state machine (docs/01 §3):
 *   starting → ready → restarting → ready | failed
 * The Vue side only consumes status; the Tauri host restarts the sidecar and
 * reports `backend-status` / `backend-failed` events. Mock mode (pure browser
 * dev) never enters this gate.
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { isMockMode, probeBackend, type BackendConfig } from '@/api'

export type BackendHealthStatus = 'starting' | 'ready' | 'restarting' | 'failed'

interface HostStatusEvent {
  status?: string
  state?: string
}

function normalizeHostStatus(payload: unknown): BackendHealthStatus | null {
  if (typeof payload === 'string') {
    if (payload === 'ready' || payload === 'starting' || payload === 'restarting' || payload === 'failed') {
      return payload
    }
    return null
  }
  if (payload && typeof payload === 'object') {
    const record = payload as HostStatusEvent
    const value = record.status ?? record.state
    if (value === 'ready' || value === 'starting' || value === 'restarting' || value === 'failed') {
      return value
    }
  }
  return null
}

export const useBackendStore = defineStore('backend', () => {
  const status = ref<BackendHealthStatus>('starting')
  const everReady = ref(false)

  const isRealMode = computed(() => !isMockMode())
  const ready = computed(() => status.value === 'ready')

  function markReady(): void {
    status.value = 'ready'
    everReady.value = true
  }

  function markFailed(): void {
    status.value = 'failed'
  }

  function markStarting(): void {
    status.value = 'starting'
  }

  function markRestarting(): void {
    status.value = 'restarting'
  }

  /** One probe round against /health; resolves when the status is updated. */
  async function probe(config?: BackendConfig): Promise<boolean> {
    const ok = await probeBackend(config)
    if (ok) markReady()
    else markFailed()
    return ok
  }

  /** Boot probe: backend is assumed starting until /health answers. */
  async function initialProbe(config?: BackendConfig): Promise<boolean> {
    if (isMockMode()) {
      markReady()
      return true
    }
    markStarting()
    return probe(config)
  }

  /** Gate retry button: shows the restarting notice while probing. */
  async function retry(config?: BackendConfig): Promise<boolean> {
    markRestarting()
    return probe(config)
  }

  /** Consume host events (Tauri Phase 3 host); unknown payloads are ignored. */
  function applyHostEvent(eventName: string, payload: unknown): void {
    if (eventName === 'backend-failed') {
      markFailed()
      return
    }
    if (eventName === 'backend-status') {
      const next = normalizeHostStatus(payload)
      if (next === 'ready') markReady()
      else if (next === 'failed') markFailed()
      else if (next === 'restarting') markRestarting()
      else if (next === 'starting') markStarting()
    }
  }

  /** Listen to Tauri events when the host API is available. */
  async function listenHostEvents(): Promise<void> {
    if (isMockMode()) return
    try {
      const { listen } = await import('@tauri-apps/api/event')
      await listen<unknown>('backend-status', (event) => applyHostEvent('backend-status', event.payload))
      await listen<unknown>('backend-failed', () => applyHostEvent('backend-failed', null))
    } catch {
      // Event API not available yet (host wiring lands in Phase 3/5) — the
      // manual probe/retry loop still drives the same gate.
    }
  }

  return {
    status,
    everReady,
    isRealMode,
    ready,
    markReady,
    markFailed,
    markStarting,
    markRestarting,
    probe,
    initialProbe,
    retry,
    applyHostEvent,
    listenHostEvents,
  }
})
