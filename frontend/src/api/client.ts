/**
 * ApiClient interface + the real fetch-based client.
 *
 * - Base path `/api/v1`, Bearer token on every business call (docs/04 §1).
 * - SSE chat streaming arrives in Phase 4; the interface method exists but the
 *   real client throws NOT_IMPLEMENTED for it in Phase 1.
 */
import type {
  AppSettings,
  Chat,
  ChatGenerationRequest,
  ChatMessage,
  ChatUpdate,
  ContextValue,
  ErrorResponse,
  FilePathRequest,
  HistoryPage,
  HistoryQuery,
  ProviderSettingsUpdate,
  ProviderSettingsView,
  ProviderTestResult,
  RuntimeCapabilities,
  TabState,
  TerminologyItem,
  TerminologyMutation,
  TranslationResponse,
  VocabularyItem,
  WordTranslation,
} from './types'

/** Config returned by the Tauri command `get_backend_config`. */
export interface BackendConfig {
  base_url: string
  token: string
  data_root: string
}

export class ApiError extends Error {
  readonly code: string
  readonly retryable: boolean
  readonly details?: Record<string, unknown>

  constructor(code: string, message: string, retryable: boolean, details?: Record<string, unknown>) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.retryable = retryable
    this.details = details
  }
}

export function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) return err
  const message = err instanceof Error ? err.message : String(err)
  return new ApiError('TRANSLATION_FAILED', message, true)
}

export interface ChatGenerationHandlers {
  onStarted?: (generationId: string) => void
  onReasoningDelta?: (delta: string) => void
  onContentDelta?: (delta: string) => void
  onCompleted?: (generationId: string) => void
  onCancelled?: (generationId: string) => void
  onError?: (error: ApiError) => void
}

export interface ChatGenerationHandle {
  readonly generationId: string
  cancel(): void
}

export interface ApiClient {
  // Runtime / settings
  getRuntimeCapabilities(): Promise<RuntimeCapabilities>
  getSettings(): Promise<AppSettings>
  updateSettings(settings: AppSettings): Promise<void>
  getProviderSettings(): Promise<ProviderSettingsView>
  updateProviderSettings(update: ProviderSettingsUpdate): Promise<void>
  testProviderConnection(): Promise<ProviderTestResult>

  // Translation
  createTranslation(
    text: string,
    forceKind?: 'word' | 'text',
    bypassCache?: boolean,
  ): Promise<TranslationResponse>
  getTranslation(id: string): Promise<TranslationResponse>
  retranslate(id: string): Promise<TranslationResponse>

  // History
  listHistory(query?: HistoryQuery): Promise<HistoryPage>
  clearHistory(): Promise<void>
  deleteHistoryItem(id: string): Promise<void>

  // Vocabulary
  listVocabulary(query?: string): Promise<VocabularyItem[]>
  saveVocabulary(lemma: string, word: WordTranslation): Promise<void>
  deleteVocabulary(lemma: string): Promise<void>

  // Terminology
  listTerminology(): Promise<TerminologyItem[]>
  createTerminology(mutation: TerminologyMutation): Promise<TerminologyItem>
  updateTerminology(id: string, mutation: TerminologyMutation): Promise<void>
  deleteTerminology(id: string): Promise<void>

  // Tabs
  getTabs(): Promise<TabState[]>
  putTabs(tabs: TabState[]): Promise<void>

  // Chats
  listChats(): Promise<Chat[]>
  createChat(): Promise<Chat>
  deleteChat(id: string): Promise<void>
  renameChat(id: string, title: string): Promise<void>
  listMessages(chatId: string): Promise<ChatMessage[]>
  clearChatMessages(chatId: string): Promise<void>
  getGlobalContext(): Promise<ContextValue>
  putGlobalContext(value: ContextValue): Promise<void>
  getConversationContext(chatId: string): Promise<ContextValue>
  putConversationContext(chatId: string, value: ContextValue): Promise<void>

  /**
   * SSE generation stream. Phase 4 for the real client; the mock client
   * simulates the stream in-memory.
   */
  streamChatGeneration(
    chatId: string,
    request: ChatGenerationRequest,
    handlers: ChatGenerationHandlers,
  ): Promise<ChatGenerationHandle>

  // Backup / data reset
  exportBackup(path: string): Promise<void>
  importBackup(path: string): Promise<void>
  clearBusinessData(): Promise<void>
  resetApp(): Promise<void>
}

export function createRealClient(config: BackendConfig): ApiClient {
  const base = config.base_url.replace(/\/+$/, '')

  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    let response: Response
    try {
      response = await fetch(`${base}${path}`, {
        ...init,
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${config.token}`,
          ...(init?.headers ?? {}),
        },
      })
    } catch (err) {
      throw new ApiError('PROVIDER_CONNECTION_FAILED', `无法连接本地后端：${String(err)}`, true)
    }

    if (response.status === 204) return undefined as T

    const text = await response.text()
    let body: unknown = undefined
    if (text.length > 0) {
      try {
        body = JSON.parse(text)
      } catch {
        body = undefined
      }
    }

    if (!response.ok) {
      const errBody = (body as ErrorResponse | undefined)?.error
      throw new ApiError(
        errBody?.code ?? 'PROVIDER_UNAVAILABLE',
        errBody?.message ?? `请求失败（HTTP ${response.status}）`,
        errBody?.retryable ?? response.status >= 500,
        errBody?.details,
      )
    }

    return body as T
  }

  const json = (payload: unknown): RequestInit['body'] => JSON.stringify(payload)

  return {
    async getRuntimeCapabilities() {
      return request<RuntimeCapabilities>('/runtime/capabilities')
    },

    async getSettings() {
      return request<AppSettings>('/settings')
    },

    async updateSettings(settings) {
      await request<void>('/settings', { method: 'PUT', body: json(settings) })
    },

    async getProviderSettings() {
      return request<ProviderSettingsView>('/settings/provider')
    },

    async updateProviderSettings(update) {
      await request<void>('/settings/provider', { method: 'PUT', body: json(update) })
    },

    async testProviderConnection() {
      return request<ProviderTestResult>('/settings/provider/test', { method: 'POST' })
    },

    async createTranslation(text, forceKind, bypassCache) {
      return request<TranslationResponse>('/translations', {
        method: 'POST',
        body: json({ text, ...(forceKind ? { force_kind: forceKind } : {}), bypass_cache: !!bypassCache }),
      })
    },

    async getTranslation(id) {
      return request<TranslationResponse>(`/translations/${encodeURIComponent(id)}`)
    },

    async retranslate(id) {
      return request<TranslationResponse>(`/translations/${encodeURIComponent(id)}/retranslate`, {
        method: 'POST',
      })
    },

    async listHistory(query) {
      const params = new URLSearchParams()
      if (query?.query) params.set('query', query.query)
      if (query?.kind) params.set('kind', query.kind)
      if (query?.limit != null) params.set('limit', String(query.limit))
      if (query?.cursor) params.set('cursor', query.cursor)
      const qs = params.toString()
      return request<HistoryPage>(`/history${qs ? `?${qs}` : ''}`)
    },

    async clearHistory() {
      await request<void>('/history', { method: 'DELETE' })
    },

    async deleteHistoryItem(id) {
      await request<void>(`/history/${encodeURIComponent(id)}`, { method: 'DELETE' })
    },

    async listVocabulary(query) {
      const qs = query ? `?query=${encodeURIComponent(query)}` : ''
      return request<VocabularyItem[]>(`/vocabulary${qs}`)
    },

    async saveVocabulary(lemma, word) {
      await request<void>(`/vocabulary/${encodeURIComponent(lemma)}`, {
        method: 'PUT',
        body: json(word),
      })
    },

    async deleteVocabulary(lemma) {
      await request<void>(`/vocabulary/${encodeURIComponent(lemma)}`, { method: 'DELETE' })
    },

    async listTerminology() {
      return request<TerminologyItem[]>('/terminology')
    },

    async createTerminology(mutation) {
      return request<TerminologyItem>('/terminology', { method: 'POST', body: json(mutation) })
    },

    async updateTerminology(id, mutation) {
      await request<void>(`/terminology/${encodeURIComponent(id)}`, {
        method: 'PUT',
        body: json(mutation),
      })
    },

    async deleteTerminology(id) {
      await request<void>(`/terminology/${encodeURIComponent(id)}`, { method: 'DELETE' })
    },

    async getTabs() {
      return request<TabState[]>('/tabs')
    },

    async putTabs(tabs) {
      await request<void>('/tabs', { method: 'PUT', body: json(tabs) })
    },

    async listChats() {
      return request<Chat[]>('/chats')
    },

    async createChat() {
      return request<Chat>('/chats', { method: 'POST' })
    },

    async deleteChat(id) {
      await request<void>(`/chats/${encodeURIComponent(id)}`, { method: 'DELETE' })
    },

    async renameChat(id, title) {
      const update: ChatUpdate = { title }
      await request<void>(`/chats/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body: json(update),
      })
    },

    async listMessages(chatId) {
      return request<ChatMessage[]>(`/chats/${encodeURIComponent(chatId)}/messages`)
    },

    async clearChatMessages(chatId) {
      await request<void>(`/chats/${encodeURIComponent(chatId)}/messages`, { method: 'DELETE' })
    },

    async getGlobalContext() {
      return request<ContextValue>('/context/global')
    },

    async putGlobalContext(value) {
      await request<void>('/context/global', { method: 'PUT', body: json(value) })
    },

    async getConversationContext(chatId) {
      return request<ContextValue>(`/chats/${encodeURIComponent(chatId)}/context`)
    },

    async putConversationContext(chatId, value) {
      await request<void>(`/chats/${encodeURIComponent(chatId)}/context`, {
        method: 'PUT',
        body: json(value),
      })
    },

    async streamChatGeneration() {
      // SSE arrives in Phase 4 (docs/04 §11). Interface is frozen now.
      throw new ApiError('NOT_IMPLEMENTED', 'SSE 流式对话将在 Phase 4 实现', false)
    },

    async exportBackup(path) {
      const body: FilePathRequest = { path }
      await request<void>('/backup/export', { method: 'POST', body: json(body) })
    },

    async importBackup(path) {
      const body: FilePathRequest = { path }
      await request<void>('/backup/import', { method: 'POST', body: json(body) })
    },

    async clearBusinessData() {
      await request<void>('/data/clear-business', { method: 'POST' })
    },

    async resetApp() {
      await request<void>('/data/reset-app', { method: 'POST' })
    },
  }
}
