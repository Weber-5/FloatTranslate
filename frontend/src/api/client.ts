/**
 * ApiClient interface + the real fetch-based client.
 *
 * - Base path `/api/v1`, Bearer token on every business call (docs/04 §1).
 * - Chat generation streams over SSE via POST fetch + ReadableStream
 *   (docs/04 §11). EventSource cannot POST, so the stream is read manually.
 */
import type {
  AppSettings,
  BackupExportResult,
  BackupImportResult,
  Chat,
  ChatGenerationRequest,
  ChatMessage,
  ChatUpdate,
  ContextValue,
  ErrorBody,
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
import { createSseParser, type SseDataEnvelope } from './sse'

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

/** Error shown when the local sidecar cannot be reached at all (backend down). */
export const BACKEND_UNAVAILABLE = 'BACKEND_UNAVAILABLE'
export const BACKEND_UNAVAILABLE_MESSAGE = '无法连接本地服务，请稍后重试'

export function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) return err
  const message = err instanceof Error ? err.message : String(err)
  return new ApiError('TRANSLATION_FAILED', message, true)
}

/**
 * No-auth readiness probe (docs/04 §1: `/health` 可不鉴权).
 * /health lives at the host root, OUTSIDE /api/v1, so a configured base_url
 * ending in /api/v1 is stripped first. Never sends the bearer token.
 */
export async function probeBackendHealth(baseUrl: string, timeoutMs = 5000): Promise<boolean> {
  const base = baseUrl
    .replace(/\/+$/, '')
    .replace(/\/api\/v1$/, '')
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(`${base}/health`, { signal: controller.signal })
    return response.ok
  } catch {
    return false
  } finally {
    clearTimeout(timer)
  }
}

export interface ChatGenerationStartedInfo {
  chat_id?: string
  model?: string
  thinking?: boolean
}

/** Payload of generation.completed (docs/04 §11). */
export interface ChatGenerationCompletedData {
  message_id?: string
  reasoning_content?: string
  content?: string
  finish_reason?: string
}

/** Terminal state of a generation stream. */
export type ChatStreamOutcome = 'completed' | 'cancelled' | 'error'

export interface ChatGenerationHandlers {
  onStarted?: (generationId: string, info?: ChatGenerationStartedInfo) => void
  onReasoningDelta?: (delta: string) => void
  onContentDelta?: (delta: string) => void
  onCompleted?: (generationId: string, data?: ChatGenerationCompletedData) => void
  onCancelled?: (generationId: string) => void
  onError?: (error: ApiError, generationId?: string) => void
}

export interface ChatGenerationHandle {
  readonly generationId: string
  /** Resolves with the terminal outcome once the stream ends. */
  readonly promise: Promise<ChatStreamOutcome>
  cancel(): void
}

export interface StreamOptions {
  /** Aborting the signal locally tears down the fetch reader. */
  signal?: AbortSignal
}

export interface ApiClient {
  // Runtime / settings
  getRuntimeCapabilities(): Promise<RuntimeCapabilities>
  getSettings(): Promise<AppSettings>
  updateSettings(settings: AppSettings): Promise<void>
  getProviderSettings(): Promise<ProviderSettingsView>
  updateProviderSettings(update: ProviderSettingsUpdate): Promise<void>
  /**
   * Tests the SUBMITTED config (docs/04 §4): the current form values travel in
   * the body — the backend tests them, not only the previously saved ones.
   */
  testProviderConnection(update: ProviderSettingsUpdate): Promise<ProviderTestResult>

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
  /** Manual compact (docs/04 §12): returns the stored compact summary. */
  compactChat(chatId: string): Promise<{ summary: string }>

  /**
   * SSE generation stream (docs/04 §11). The real client reads the POST
   * response body via fetch + ReadableStream; the mock client simulates the
   * same event lifecycle with timers.
   */
  streamChatGeneration(
    chatId: string,
    request: ChatGenerationRequest,
    handlers: ChatGenerationHandlers,
    options?: StreamOptions,
  ): Promise<ChatGenerationHandle>

  /** Regenerates the last assistant answer and streams the new one (docs/04 §11). */
  regenerateChat(
    chatId: string,
    handlers: ChatGenerationHandlers,
    options?: StreamOptions,
  ): Promise<ChatGenerationHandle>

  // Backup / data reset
  exportBackup(path: string): Promise<BackupExportResult>
  importBackup(path: string): Promise<BackupImportResult>
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
    } catch {
      // Backend down / sidecar not ready: uniform typed error for the stores.
      throw new ApiError(BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE, true)
    }

    if (response.status === 204) return undefined as T

    const text = await response.text()
    let body: unknown = undefined
    let isJson = false
    if (text.length > 0) {
      try {
        body = JSON.parse(text)
        isJson = true
      } catch {
        body = undefined
        isJson = false
      }
    }

    if (!response.ok) {
      const errBody = isJson ? (body as ErrorResponse | undefined)?.error : undefined
      if (errBody?.code) {
        throw new ApiError(
          errBody.code,
          errBody.message,
          errBody.retryable ?? response.status >= 500,
          errBody.details,
        )
      }
      // Non-JSON failure (proxy error page / crashed process) → backend down.
      throw new ApiError(BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE, true)
    }

    return body as T
  }

  const json = (payload: unknown): RequestInit['body'] => JSON.stringify(payload)

  /**
   * Opens the SSE POST and validates the response. Rejects with ApiError for
   * transport failures and HTTP error envelopes (e.g. 409
   * GENERATION_ALREADY_ACTIVE) BEFORE any stream is consumed.
   */
  async function openStream(
    path: string,
    body: unknown,
    signal: AbortSignal,
  ): Promise<ReadableStreamDefaultReader<Uint8Array>> {
    let response: Response
    try {
      response = await fetch(`${base}${path}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'text/event-stream',
          Authorization: `Bearer ${config.token}`,
        },
        body: json(body),
        signal,
      })
    } catch {
      if (signal.aborted) {
        // Local abort before the response arrived: the caller knows it
        // cancelled; nothing to stream.
        throw new ApiError('REQUEST_ABORTED', '已取消', false)
      }
      throw new ApiError(BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE, true)
    }

    if (!response.ok) {
      const text = await response.text().catch(() => '')
      let errBody: ErrorBody | undefined
      try {
        errBody = text.length > 0 ? (JSON.parse(text) as ErrorResponse).error : undefined
      } catch {
        errBody = undefined
      }
      if (errBody?.code) {
        throw new ApiError(
          errBody.code,
          errBody.message,
          errBody.retryable ?? response.status >= 500,
          errBody.details,
        )
      }
      throw new ApiError(BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE, true)
    }
    if (!response.body) {
      throw new ApiError(BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE, true)
    }
    return response.body.getReader()
  }

  /**
   * Reads one SSE body to completion and dispatches events to the handlers.
   * The returned promise settles as soon as a terminal event, an abort or a
   * transport failure is seen — the reader loop itself may still be unwinding.
   */
  function pumpChatStream(
    reader: ReadableStreamDefaultReader<Uint8Array>,
    handlers: ChatGenerationHandlers,
    options?: StreamOptions,
  ): Promise<ChatStreamOutcome> {
    const decoder = new TextDecoder()
    let outcome: ChatStreamOutcome | null = null
    let generationId = ''
    let resolveOutcome!: (value: ChatStreamOutcome) => void
    const finished = new Promise<ChatStreamOutcome>((resolve) => {
      resolveOutcome = resolve
    })

    const settle = (next: ChatStreamOutcome): void => {
      if (outcome) return
      outcome = next
      resolveOutcome(next)
    }

    const parser = createSseParser(({ event, data }) => {
      const envelope = (data ?? {}) as SseDataEnvelope
      const payload = (envelope.data ?? {}) as Record<string, unknown>
      if (typeof envelope.generation_id === 'string' && envelope.generation_id.length > 0) {
        generationId = envelope.generation_id
      }
      const asText = (value: unknown): string => (typeof value === 'string' ? value : '')
      switch (event) {
        case 'generation.started':
          handlers.onStarted?.(generationId, {
            chat_id: asText(payload.chat_id),
            model: asText(payload.model),
            thinking: payload.thinking === true,
          })
          break
        case 'reasoning.delta':
          handlers.onReasoningDelta?.(asText(payload.text))
          break
        case 'content.delta':
          handlers.onContentDelta?.(asText(payload.text))
          break
        case 'generation.completed':
          handlers.onCompleted?.(generationId, {
            message_id: asText(payload.message_id) || undefined,
            reasoning_content: asText(payload.reasoning_content) || undefined,
            content: asText(payload.content) || undefined,
            finish_reason: asText(payload.finish_reason) || undefined,
          })
          settle('completed')
          break
        case 'generation.cancelled':
          handlers.onCancelled?.(generationId)
          settle('cancelled')
          break
        case 'generation.error': {
          const error = new ApiError(
            asText(payload.code) || 'GENERATION_FAILED',
            asText(payload.message) || '生成失败',
            payload.retryable === true,
          )
          handlers.onError?.(error, generationId)
          settle('error')
          break
        }
        default:
          // Unknown event types are ignored for forward compatibility.
          break
      }
    })

    // Abort settles the promise immediately; the reader loop below unwinds
    // on its own (a real fetch rejects the pending read on abort).
    const abort = () => settle('cancelled')
    options?.signal?.addEventListener('abort', abort, { once: true })

    void (async () => {
      try {
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          parser.push(decoder.decode(value, { stream: true }))
          if (outcome) break
        }
        parser.flush()
      } catch {
        if (!outcome) {
          if (options?.signal?.aborted) {
            handlers.onCancelled?.(generationId)
          } else {
            handlers.onError?.(
              new ApiError(BACKEND_UNAVAILABLE, BACKEND_UNAVAILABLE_MESSAGE, true),
              generationId || undefined,
            )
          }
          settle(options?.signal?.aborted ? 'cancelled' : 'error')
        }
      } finally {
        options?.signal?.removeEventListener('abort', abort)
        // Release the connection when the stream ends early (cancel path).
        reader.cancel().catch(() => undefined)
      }

      if (!outcome) {
        // Server closed the stream without a terminal event: surface as
        // error so the store never waits forever.
        handlers.onError?.(new ApiError('STREAM_CLOSED', '生成流意外中断，请重试。', true), generationId || undefined)
        settle('error')
      }
    })()

    return finished
  }

  /** Shared wiring: local abort also asks the backend to cancel (docs/04 §11). */
  async function openGenerationStream(
    chatId: string,
    path: string,
    body: unknown,
    handlers: ChatGenerationHandlers,
    options?: StreamOptions,
  ): Promise<ChatGenerationHandle> {
    const controller = new AbortController()
    const onExternalAbort = () => controller.abort()
    options?.signal?.addEventListener('abort', onExternalAbort, { once: true })

    // generation_id only becomes known when generation.started arrives; the
    // cancel endpoint needs it, so capture it transparently.
    let generationId = ''
    const wrapped: ChatGenerationHandlers = {
      ...handlers,
      onStarted: (id, info) => {
        generationId = id
        handlers.onStarted?.(id, info)
      },
    }

    // Resolves once the response is validated — errors (409, backend down)
    // propagate to the caller here, before any stream event.
    let reader: ReadableStreamDefaultReader<Uint8Array>
    try {
      reader = await openStream(path, body, controller.signal)
    } catch (err) {
      options?.signal?.removeEventListener('abort', onExternalAbort)
      if (err instanceof ApiError && err.code === 'REQUEST_ABORTED') {
        // Cancelled before the stream existed — no backend cancel needed.
        handlers.onCancelled?.('')
        return {
          generationId: '',
          promise: Promise.resolve<ChatStreamOutcome>('cancelled'),
          cancel() {
            /* already cancelled */
          },
        }
      }
      throw err
    }
    const promise = pumpChatStream(reader, wrapped, { signal: controller.signal }).finally(() => {
      options?.signal?.removeEventListener('abort', onExternalAbort)
    })

    return {
      get generationId() {
        return generationId
      },
      promise,
      cancel() {
        controller.abort()
        if (generationId.length === 0) return
        const cancelPath =
          `/chats/${encodeURIComponent(chatId)}` +
          `/generations/${encodeURIComponent(generationId)}/cancel`
        // Best-effort: the backend answers 202; failures are non-fatal
        // because the local stream is already torn down.
        void fetch(`${base}${cancelPath}`, {
          method: 'POST',
          headers: { Authorization: `Bearer ${config.token}` },
        }).catch(() => undefined)
      },
    }
  }

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

    async testProviderConnection(update: ProviderSettingsUpdate) {
      return request<ProviderTestResult>('/settings/provider/test', {
        method: 'POST',
        body: json({
          mode: update.mode,
          base_url: update.base_url,
          translation_model: update.translation_model,
          chat_model: update.chat_model,
          ...(update.api_key && update.api_key.trim().length > 0 ? { api_key: update.api_key } : {}),
        }),
      })
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

    async compactChat(chatId) {
      return request<{ summary: string }>(`/chats/${encodeURIComponent(chatId)}/compact`, {
        method: 'POST',
      })
    },

    async streamChatGeneration(chatId, requestBody, handlers, options) {
      return openGenerationStream(
        chatId,
        `/chats/${encodeURIComponent(chatId)}/generations`,
        requestBody,
        handlers,
        options,
      )
    },

    async regenerateChat(chatId, handlers, options) {
      return openGenerationStream(
        chatId,
        `/chats/${encodeURIComponent(chatId)}/regenerate`,
        {},
        handlers,
        options,
      )
    },

    async exportBackup(path) {
      const body: FilePathRequest = { path }
      return request<BackupExportResult>('/backup/export', { method: 'POST', body: json(body) })
    },

    async importBackup(path) {
      const body: FilePathRequest = { path }
      return request<BackupImportResult>('/backup/import', { method: 'POST', body: json(body) })
    },

    async clearBusinessData() {
      await request<void>('/data/clear-business', { method: 'POST' })
    },

    async resetApp() {
      await request<void>('/data/reset-app', { method: 'POST' })
    },
  }
}
