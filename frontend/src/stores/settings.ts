/**
 * Settings store: app settings + provider settings + runtime capabilities.
 * Exposes the "configured vs effective" clamp notice state (docs/06 §9) and
 * terminology CRUD (docs/00 §9).
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type {
  AppSettings,
  ProviderSettingsUpdate,
  ProviderSettingsView,
  ProviderTestResult,
  RuntimeCapabilities,
  TerminologyItem,
} from '@/api/types'
import { useApi, toApiError } from '@/api'
import { isMockMode } from '@/api'

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error'
export type ListStatus = 'idle' | 'loading' | 'success' | 'empty' | 'error'

export const useSettingsStore = defineStore('settings', () => {
  const app = ref<AppSettings | null>(null)
  const provider = ref<ProviderSettingsView | null>(null)
  const capabilities = ref<RuntimeCapabilities | null>(null)
  const status = ref<LoadStatus>('idle')
  const loadError = ref<string | null>(null)

  const savingApp = ref(false)
  const savingProvider = ref(false)
  const providerTestRunning = ref(false)
  const providerTestResult = ref<ProviderTestResult | null>(null)

  const globalContext = ref('')
  const globalContextSaving = ref(false)

  const terminology = ref<TerminologyItem[]>([])
  const terminologyStatus = ref<ListStatus>('idle')
  const terminologyError = ref<string | null>(null)

  const mockMode = computed(() => isMockMode())

  const needsOnboarding = computed(() => provider.value !== null && !provider.value.api_key_configured)

  /** Non-null when the provider clamps configured context/output limits. */
  const clampNotice = computed(() => {
    const caps = capabilities.value
    const settings = app.value
    if (!caps || !settings) return null
    const configuredContext = settings.context_tokens
    const effectiveContext = caps.effective_context_tokens
    const configuredOutput = settings.output_tokens
    const effectiveOutput = caps.effective_output_tokens
    const contextClamped =
      configuredContext != null && effectiveContext != null && effectiveContext < configuredContext
    const outputClamped =
      configuredOutput != null && effectiveOutput != null && effectiveOutput < configuredOutput
    if (!contextClamped && !outputClamped) return null
    return {
      configured: configuredContext ?? 0,
      effective: contextClamped ? effectiveContext : configuredContext ?? 0,
      configuredOut: configuredOutput ?? 0,
      effectiveOut: outputClamped ? effectiveOutput : configuredOutput ?? 0,
    }
  })

  async function load(force = false): Promise<void> {
    if (status.value === 'success' && !force) return
    status.value = 'loading'
    loadError.value = null
    try {
      const api = useApi()
      const [appSettings, providerView, caps, globalCtx] = await Promise.all([
        api.getSettings(),
        api.getProviderSettings(),
        api.getRuntimeCapabilities(),
        api.getGlobalContext().catch(() => ({ content: '' })),
      ])
      app.value = appSettings
      provider.value = providerView
      capabilities.value = caps
      globalContext.value = globalCtx.content
      status.value = 'success'
    } catch (err) {
      loadError.value = toApiError(err).message
      status.value = 'error'
    }
  }

  async function saveApp(partial: Partial<AppSettings>): Promise<void> {
    const api = useApi()
    savingApp.value = true
    try {
      const merged: AppSettings = { ...(app.value ?? {}), ...partial }
      await api.updateSettings(merged)
      app.value = merged
    } finally {
      savingApp.value = false
    }
  }

  async function saveProvider(update: ProviderSettingsUpdate): Promise<void> {
    const api = useApi()
    savingProvider.value = true
    try {
      await api.updateProviderSettings(update)
      provider.value = await api.getProviderSettings()
    } finally {
      savingProvider.value = false
    }
  }

  async function testConnection(): Promise<ProviderTestResult> {
    providerTestRunning.value = true
    providerTestResult.value = null
    try {
      providerTestResult.value = await useApi().testProviderConnection()
      return providerTestResult.value
    } finally {
      providerTestRunning.value = false
    }
  }

  async function saveGlobalContext(content: string): Promise<void> {
    globalContextSaving.value = true
    try {
      await useApi().putGlobalContext({ content })
      globalContext.value = content
    } finally {
      globalContextSaving.value = false
    }
  }

  async function loadTerminology(): Promise<void> {
    terminologyStatus.value = 'loading'
    terminologyError.value = null
    try {
      const items = await useApi().listTerminology()
      terminology.value = items
      terminologyStatus.value = items.length === 0 ? 'empty' : 'success'
    } catch (err) {
      terminologyError.value = toApiError(err).message
      terminologyStatus.value = 'error'
    }
  }

  async function addTerm(source: string, target: string): Promise<void> {
    const item = await useApi().createTerminology({ source, target })
    terminology.value = [...terminology.value, item]
    terminologyStatus.value = 'success'
  }

  async function updateTerm(id: string, source: string, target: string): Promise<void> {
    await useApi().updateTerminology(id, { source, target })
    terminology.value = terminology.value.map((item) =>
      item.id === id ? { ...item, source, target } : item,
    )
  }

  async function deleteTerm(id: string): Promise<void> {
    await useApi().deleteTerminology(id)
    terminology.value = terminology.value.filter((item) => item.id !== id)
    if (terminology.value.length === 0) terminologyStatus.value = 'empty'
  }

  /** After data reset the whole store must be refetched. */
  function invalidate(): void {
    status.value = 'idle'
    app.value = null
    provider.value = null
    capabilities.value = null
    providerTestResult.value = null
  }

  return {
    app,
    provider,
    capabilities,
    status,
    loadError,
    savingApp,
    savingProvider,
    providerTestRunning,
    providerTestResult,
    globalContext,
    globalContextSaving,
    terminology,
    terminologyStatus,
    terminologyError,
    mockMode,
    needsOnboarding,
    clampNotice,
    load,
    saveApp,
    saveProvider,
    testConnection,
    saveGlobalContext,
    loadTerminology,
    addTerm,
    updateTerm,
    deleteTerm,
    invalidate,
  }
})
