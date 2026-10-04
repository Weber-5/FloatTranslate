/**
 * In-memory mock ApiClient for pure-browser dev (`npm run dev`).
 *
 * - Deterministic data, no network calls, no LLM.
 * - "suspended" returns exactly the frozen contract WordTranslation.
 * - Multi-word text returns one TextTranslation segment per input paragraph;
 *   known glossary words are replaced by canned Chinese glosses, the rest are
 *   kept verbatim.
 * - Any input containing "trigger-error" fails with a retryable TRANSLATION_FAILED
 *   error so the retryable-error state machine can be demoed and tested.
 */
import type {
  AppSettings,
  Chat,
  ChatGenerationRequest,
  ChatMessage,
  ContextValue,
  HistoryItem,
  HistoryPage,
  HistoryQuery,
  ProviderSettingsView,
  ProviderSettingsUpdate,
  ProviderTestResult,
  RuntimeCapabilities,
  TabState,
  TerminologyItem,
  TerminologyMutation,
  TranslationKind,
  TranslationResponse,
  VocabularyItem,
  WordTranslation,
} from './types'
import type { ApiClient, ChatGenerationHandle, ChatGenerationHandlers, ChatStreamOutcome, StreamOptions } from './client'
import { ApiError } from './client'
import { buildWordTranslation, GLOSS, SURFACE_TO_LEMMA } from '@/lib/wordData'
import { DEFAULT_AI_SYSTEM_PROMPT } from '@/constants'

let counter = 0
function nextId(prefix: string): string {
  counter += 1
  return `${prefix}-${String(counter).padStart(4, '0')}`
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

const TRANSLATION_MODEL = 'deepseek-flash'
const CONFIG_HASH = 'mock-config-v1'

function isSingleWord(text: string): boolean {
  const trimmed = text.trim()
  if (/\s/.test(trimmed)) return false
  return /^[A-Za-z][A-Za-z'’-]*$/.test(trimmed)
}

function normalize(text: string): string {
  return text.trim().toLowerCase().replace(/\s+/g, ' ')
}

function translateParagraph(paragraph: string): string {
  return paragraph.replace(/[A-Za-z][A-Za-z'’-]*/g, (token) => {
    const lower = token.toLowerCase()
    const lemma = SURFACE_TO_LEMMA.get(lower) ?? lower
    return GLOSS.get(lemma) ?? token
  })
}

interface MockTextResult {
  source_markdown: string
  translated_markdown: string
  segments: { source: string; translation: string }[]
}

function mockTranslateText(text: string): MockTextResult {
  const paragraphs = text
    .split(/\n+/)
    .map((p) => p.trim())
    .filter((p) => p.length > 0)
  const segments = paragraphs.map((paragraph) => ({
    source: paragraph,
    translation: translateParagraph(paragraph),
  }))
  return {
    source_markdown: text,
    translated_markdown: segments.map((s) => s.translation).join('\n\n'),
    segments,
  }
}

interface MockState {
  settings: AppSettings
  provider: ProviderSettingsView
  capabilities: RuntimeCapabilities
  cache: Map<string, TranslationResponse>
  history: HistoryItem[]
  responses: Map<string, TranslationResponse>
  vocabulary: Map<string, VocabularyItem>
  terminology: TerminologyItem[]
  tabs: TabState[]
  chats: Chat[]
  messages: Map<string, ChatMessage[]>
  conversationContext: Map<string, string>
  globalContext: string
  /** Chat ids with a generation in flight (mirrors backend 409 behaviour). */
  activeGenerations: Set<string>
  /** Thinking flag of the last generation per chat, reused by regenerate. */
  lastThinking: Map<string, boolean>
}

function defaultSettings(): AppSettings {
  return {
    theme: 'system',
    always_on_top: true,
    auto_start: false,
    hotkey_toggle_window: 'Ctrl+Alt+Space',
    hotkey_quick_translate: 'Ctrl+Alt+Q',
    context_tokens: 1000000,
    output_tokens: 4096,
    auto_compact: true,
    compact_threshold: 80,
    ai_system_prompt: DEFAULT_AI_SYSTEM_PROMPT,
    custom_translation_prompt: '',
    proxy_mode: 'system',
    proxy_host: '',
    proxy_port: 0,
    proxy_username: '',
    proxy_password: '',
  }
}

function defaultProvider(): ProviderSettingsView {
  return {
    mode: 'deepseek',
    base_url: 'https://api.deepseek.com',
    translation_model: TRANSLATION_MODEL,
    chat_model: TRANSLATION_MODEL,
    api_key_configured: true,
    api_key_hint: 'sk-****mock',
  }
}

function defaultCapabilities(): RuntimeCapabilities {
  // Deliberately clamps 1,000,000 -> 128,000 so the Settings
  // "configured vs effective" notice is demoable.
  return {
    configured_context_tokens: 1000000,
    effective_context_tokens: 128000,
    configured_output_tokens: 4096,
    effective_output_tokens: 4096,
    supports_thinking: true,
    supports_structured_output: true,
  }
}

function createInitialState(): MockState {
  const chatId = nextId('chat')
  const now = new Date().toISOString()
  return {
    settings: defaultSettings(),
    provider: defaultProvider(),
    capabilities: defaultCapabilities(),
    cache: new Map(),
    history: [],
    responses: new Map(),
    vocabulary: new Map(),
    terminology: [
      { id: nextId('term'), source: 'FloatTranslate', target: '浮译' },
      { id: nextId('term'), source: 'Structured Output', target: '结构化输出' },
    ],
    tabs: [],
    chats: [{ id: chatId, title: '新会话', created_at: now, updated_at: now }],
    messages: new Map([[chatId, []]]),
    conversationContext: new Map([[chatId, '']]),
    globalContext: '',
    activeGenerations: new Set(),
    lastThinking: new Map(),
  }
}

function buildMockReply(content: string): string {
  return [
    '这是 Mock 模式的本地回答。',
    '',
    `你发送了「${content}」。Phase 1 骨架用模拟流验证多会话、思考开关与 slash 命令的交互；`,
    'Phase 4 将通过 SSE 接入真实模型输出。',
  ].join('\n')
}

export function createMockClient(): ApiClient {
  const state = createInitialState()

  async function translateOnce(text: string, bypassCache: boolean): Promise<TranslationResponse> {
    const trimmed = text.trim()
    if (trimmed.length === 0) {
      throw new ApiError('INVALID_REQUEST', '输入内容不能为空', false)
    }
    if (!/[A-Za-z]/.test(trimmed)) {
      throw new ApiError('UNSUPPORTED_LANGUAGE', 'FloatTranslate 1.0 暂仅支持英译中', false)
    }
    if (/trigger[- ]?error/i.test(trimmed)) {
      throw new ApiError('TRANSLATION_FAILED', '模拟的 Provider 错误：请点击重试。', true)
    }

    const kind: TranslationKind = isSingleWord(trimmed) ? 'word' : 'text'
    const cacheKey = `${normalize(trimmed)}|${kind}|${TRANSLATION_MODEL}|${CONFIG_HASH}`

    if (!bypassCache) {
      const cached = state.cache.get(cacheKey)
      if (cached) {
        // Cache hit: same result, but the response is labeled as cache and
        // still recorded in history (source badge: cache).
        const now = new Date().toISOString()
        state.history.unshift({
          id: cached.translation_id,
          kind: cached.kind,
          input_text: trimmed,
          result: cached.result,
          source: 'cache',
          model: cached.model,
          created_at: now,
        })
        return { ...cached, source: 'cache' }
      }
    }

    await sleep(200)

    const now = new Date().toISOString()
    const result =
      kind === 'word' ? buildWordTranslation(trimmed) : mockTranslateText(trimmed)
    const response: TranslationResponse = {
      translation_id: nextId('tr'),
      kind,
      source: 'model',
      result,
      model: TRANSLATION_MODEL,
      created_at: now,
    }
    state.cache.set(cacheKey, response)
    state.responses.set(response.translation_id, response)
    state.history.unshift({
      id: response.translation_id,
      kind,
      input_text: trimmed,
      result,
      source: 'model',
      model: TRANSLATION_MODEL,
      created_at: now,
    })
    return response
  }

  /** Chats whose trigger-error already fired: the failure is transient so
   * the retryable-error → retry flow can be demoed and tested. */
  const transientErrorFired = new Set<string>()

  const MOCK_REASONING =
    '先拆解用户的问题，确认它是在询问解释还是翻译，再组织一段简洁的中文回答。'

  function appendMockMessage(chatId: string, message: ChatMessage): void {
    if (!state.messages.has(chatId)) state.messages.set(chatId, [])
    state.messages.get(chatId)!.push(message)
    const chat = state.chats.find((entry) => entry.id === chatId)
    if (chat) chat.updated_at = message.created_at
  }

  /**
   * Builds one simulated generation: schedules the whole started → deltas →
   * completed timeline on timers and returns a handler-accepting starter.
   * `cancel()` on the handle stops at the next tick and emits cancelled,
   * keeping whatever content was already streamed.
   */
  function runMockGeneration(
    chatId: string,
    content: string,
    opts: { thinking: boolean; forceError: boolean },
  ): (handlers: ChatGenerationHandlers) => Promise<ChatGenerationHandle> {
    return (handlers) => {
      const generationId = nextId('gen')
      const timers: ReturnType<typeof setTimeout>[] = []
      let finished = false
      let emittedContent = ''
      let resolvePromise!: (value: ChatStreamOutcome) => void
      const promise = new Promise<ChatStreamOutcome>((resolve) => {
        resolvePromise = resolve
      })

      function finish(outcome: ChatStreamOutcome): void {
        if (finished) return
        finished = true
        for (const timer of timers) clearTimeout(timer)
        timers.length = 0
        state.activeGenerations.delete(chatId)
        resolvePromise(outcome)
      }

      const reasoning = opts.thinking ? MOCK_REASONING : ''

      type Step = { delay: number; kind: 'reasoning' | 'content'; text: string }
      const steps: Step[] = []
      let delay = 150
      timers.push(
        setTimeout(() => {
          if (finished) return
          handlers.onStarted?.(generationId, {
            chat_id: chatId,
            model: TRANSLATION_MODEL,
            thinking: opts.thinking,
          })
        }, delay),
      )
      delay += 100
      if (reasoning) {
        for (const chunk of reasoning.match(/.{1,12}/gu) ?? []) {
          steps.push({ delay, kind: 'reasoning', text: chunk })
          delay += 60
        }
      }
      const words = content.match(/\S+\s*/gsu) ?? [content]
      let emittedWords = 0
      for (const word of words) {
        steps.push({ delay, kind: 'content', text: word })
        delay += 60
        emittedWords += 1
        if (opts.forceError && emittedWords === 2) {
          steps.push({ delay, kind: 'content', text: '' }) // placeholder keeps ordering readable
          delay += 60
          break
        }
      }

      for (const step of steps) {
        timers.push(
          setTimeout(() => {
            if (finished) return
            if (step.kind === 'reasoning') {
              handlers.onReasoningDelta?.(step.text)
            } else if (step.text) {
              emittedContent += step.text
              handlers.onContentDelta?.(step.text)
            } else if (opts.forceError) {
              handlers.onError?.(
                new ApiError('TRANSLATION_FAILED', '模拟的 Provider 错误：请点击重试。', true),
                generationId,
              )
              finish('error')
            }
          }, step.delay),
        )
      }

      timers.push(
        setTimeout(() => {
          if (finished) return
          const now = new Date().toISOString()
          const message: ChatMessage = {
            id: nextId('msg'),
            role: 'assistant',
            content,
            ...(reasoning ? { reasoning_content: reasoning } : {}),
            created_at: now,
          }
          appendMockMessage(chatId, message)
          handlers.onCompleted?.(generationId, {
            message_id: message.id,
            reasoning_content: reasoning || undefined,
            content,
            finish_reason: 'stop',
          })
          finish('completed')
        }, delay + 80),
      )

      return Promise.resolve({
        generationId,
        promise,
        cancel() {
          if (finished) return
          for (const timer of timers) clearTimeout(timer)
          timers.length = 0
          // Stop at the NEXT tick, like a real stream that still has to
          // deliver the generation.cancelled frame.
          timers.push(
            setTimeout(() => {
              if (finished) return
              if (emittedContent.length > 0) {
                appendMockMessage(chatId, {
                  id: nextId('msg'),
                  role: 'assistant',
                  content: emittedContent,
                  created_at: new Date().toISOString(),
                })
              }
              handlers.onCancelled?.(generationId)
              finish('cancelled')
            }, 0),
          )
        },
      })
    }
  }

  const client: ApiClient = {
    async getRuntimeCapabilities() {
      return { ...state.capabilities }
    },

    async getSettings() {
      return { ...state.settings }
    },

    async updateSettings(settings) {
      state.settings = { ...state.settings, ...settings }
    },

    async getProviderSettings() {
      return { ...state.provider }
    },

    async updateProviderSettings(update: ProviderSettingsUpdate) {
      state.provider = {
        mode: update.mode,
        base_url: update.base_url,
        translation_model: update.translation_model,
        chat_model: update.chat_model,
        api_key_configured: state.provider.api_key_configured,
        api_key_hint: state.provider.api_key_hint,
      }
      if (update.api_key && update.api_key.trim().length > 0) {
        const key = update.api_key.trim()
        state.provider.api_key_configured = true
        state.provider.api_key_hint = `${key.slice(0, 2)}****${key.slice(-2)}`
      }
    },

    async testProviderConnection(update: ProviderSettingsUpdate): Promise<ProviderTestResult> {
      await sleep(400)
      // The backend tests the SUBMITTED config, not the saved one (docs/04 §4).
      const submittedKey = update.api_key?.trim() ?? ''
      if (!submittedKey && !state.provider.api_key_configured) {
        return { ok: false, message: '尚未配置 API Key' }
      }
      if (submittedKey === 'invalid-key') {
        return { ok: false, message: 'Mock：API Key 无效' }
      }
      if (!update.base_url.trim()) {
        return { ok: false, message: 'Mock：Base URL 不能为空' }
      }
      return { ok: true, message: 'Mock 连接成功（未发起任何网络请求）' }
    },

    async createTranslation(text, _forceKind, bypassCache) {
      return translateOnce(text, !!bypassCache)
    },

    async getTranslation(id) {
      const response = state.responses.get(id)
      if (!response) throw new ApiError('NOT_FOUND', '翻译记录不存在', false)
      return { ...response }
    },

    async retranslate(id) {
      const previous = state.responses.get(id)
      if (!previous) throw new ApiError('NOT_FOUND', '翻译记录不存在', false)
      const input =
        previous.kind === 'word'
          ? (previous.result as WordTranslation).word
          : (previous.result as { source_markdown: string }).source_markdown
      const fresh = await translateOnce(input, true)
      // Retranslate overwrites the history result and cache entry (docs/00 §6).
      const historyItem = state.history.find((item) => item.id === id)
      if (historyItem) historyItem.result = fresh.result
      return fresh
    },

    async listHistory(query?: HistoryQuery): Promise<HistoryPage> {
      let items = [...state.history]
      if (query?.kind) items = items.filter((item) => item.kind === query.kind)
      if (query?.query) {
        const q = query.query.toLowerCase()
        items = items.filter((item) => item.input_text.toLowerCase().includes(q))
      }
      items.sort((a, b) => b.created_at.localeCompare(a.created_at))
      const limit = query?.limit ?? 50
      const start = query?.cursor ? Number(query.cursor) : 0
      const page = items.slice(start, start + limit)
      const next = start + limit < items.length ? String(start + limit) : null
      return { items: page, next_cursor: next }
    },

    async clearHistory() {
      state.history = []
    },

    async deleteHistoryItem(id) {
      state.history = state.history.filter((item) => item.id !== id)
    },

    async listVocabulary(query?: string) {
      let items = [...state.vocabulary.values()]
      if (query && query.trim().length > 0) {
        const q = query.trim().toLowerCase()
        items = items.filter(
          (item) =>
            item.lemma.toLowerCase().includes(q) || item.word.word.toLowerCase().includes(q),
        )
      }
      items.sort((a, b) => b.saved_at.localeCompare(a.saved_at))
      return items
    },

    async saveVocabulary(lemma, word) {
      const now = new Date().toISOString()
      const key = lemma.toLowerCase()
      const existing = state.vocabulary.get(key)
      if (existing) {
        existing.last_viewed_at = now
        existing.word = word
        return
      }
      state.vocabulary.set(key, {
        lemma: key,
        word,
        saved_at: now,
        last_viewed_at: now,
      })
    },

    async deleteVocabulary(lemma) {
      state.vocabulary.delete(lemma.toLowerCase())
    },

    async listTerminology() {
      return state.terminology.map((item) => ({ ...item }))
    },

    async createTerminology(mutation: TerminologyMutation) {
      const source = mutation.source.trim()
      const target = mutation.target.trim()
      if (!source || !target) {
        throw new ApiError('INVALID_REQUEST', '术语原文和译文不能为空', false)
      }
      const conflict = state.terminology.find(
        (item) => item.source.toLowerCase() === source.toLowerCase(),
      )
      if (conflict) {
        throw new ApiError('CONFLICT', `术语「${source}」已存在`, false)
      }
      const item: TerminologyItem = { id: nextId('term'), source, target }
      state.terminology.push(item)
      return { ...item }
    },

    async updateTerminology(id, mutation) {
      const item = state.terminology.find((entry) => entry.id === id)
      if (!item) throw new ApiError('NOT_FOUND', '术语不存在', false)
      item.source = mutation.source.trim()
      item.target = mutation.target.trim()
    },

    async deleteTerminology(id) {
      state.terminology = state.terminology.filter((item) => item.id !== id)
    },

    async getTabs() {
      return state.tabs.map((tab) => ({ ...tab, payload: { ...tab.payload } }))
    },

    async putTabs(tabs) {
      state.tabs = tabs.map((tab) => ({ ...tab, payload: { ...tab.payload } }))
    },

    async listChats() {
      return state.chats.map((chat) => ({ ...chat }))
    },

    async createChat() {
      const now = new Date().toISOString()
      const chat: Chat = {
        id: nextId('chat'),
        title: '新会话',
        created_at: now,
        updated_at: now,
      }
      state.chats.unshift(chat)
      state.messages.set(chat.id, [])
      state.conversationContext.set(chat.id, '')
      return { ...chat }
    },

    async deleteChat(id) {
      state.chats = state.chats.filter((chat) => chat.id !== id)
      state.messages.delete(id)
      state.conversationContext.delete(id)
    },

    async renameChat(id, title) {
      const chat = state.chats.find((entry) => entry.id === id)
      if (!chat) throw new ApiError('NOT_FOUND', '会话不存在', false)
      chat.title = title
      chat.updated_at = new Date().toISOString()
    },

    async listMessages(chatId) {
      return [...(state.messages.get(chatId) ?? [])].map((message) => ({ ...message }))
    },

    async clearChatMessages(chatId) {
      state.messages.set(chatId, [])
      const chat = state.chats.find((entry) => entry.id === chatId)
      if (chat) chat.updated_at = new Date().toISOString()
      // Compact summary would be cleared here; conversation context is kept.
    },

    async getGlobalContext() {
      return { content: state.globalContext }
    },

    async putGlobalContext(value: ContextValue) {
      state.globalContext = value.content
    },

    async getConversationContext(chatId) {
      return { content: state.conversationContext.get(chatId) ?? '' }
    },

    async putConversationContext(chatId, value) {
      state.conversationContext.set(chatId, value.content)
    },

    /**
     * Simulates the full SSE generation lifecycle (docs/04 §11) with timers:
     * started → reasoning.delta (if thinking) → content.delta word-by-word →
     * completed. cancel() stops at the NEXT tick and emits cancelled, like a
     * real stream would. Scriptable failure hooks for tests:
     * - content containing "trigger-409" → 409 GENERATION_ALREADY_ACTIVE
     * - content containing "trigger-error" → generation.error mid-stream
     * A second concurrent generation on the same chat also yields the 409.
     */
    async streamChatGeneration(
      chatId: string,
      request: ChatGenerationRequest,
      handlers: ChatGenerationHandlers,
      _options?: StreamOptions,
    ): Promise<ChatGenerationHandle> {
      if (/trigger[- ]?409/i.test(request.content)) {
        throw new ApiError('GENERATION_ALREADY_ACTIVE', '该会话已有生成任务进行中', false, {
          http_status: 409,
        })
      }
      if (state.activeGenerations.has(chatId)) {
        throw new ApiError('GENERATION_ALREADY_ACTIVE', '该会话已有生成任务进行中', false, {
          http_status: 409,
        })
      }
      state.activeGenerations.add(chatId)
      state.lastThinking.set(chatId, request.thinking === true)

      // The backend persists the user message when POST /generations lands.
      if (!state.messages.has(chatId)) state.messages.set(chatId, [])
      state.messages.get(chatId)!.push({
        id: nextId('msg'),
        role: 'user',
        content: request.content,
        created_at: new Date().toISOString(),
      })

      // "trigger-error" fails the FIRST attempt only (retryable semantics),
      // so retry() can complete the turn.
      const wantsError = /trigger[- ]?error/i.test(request.content)
      const forceError = wantsError && !transientErrorFired.has(chatId)
      if (wantsError) transientErrorFired.add(chatId)
      return runMockGeneration(chatId, buildMockReply(request.content), {
        thinking: request.thinking === true,
        forceError,
      })(handlers)
    },

    async regenerateChat(
      chatId: string,
      handlers: ChatGenerationHandlers,
      _options?: StreamOptions,
    ): Promise<ChatGenerationHandle> {
      if (state.activeGenerations.has(chatId)) {
        throw new ApiError('GENERATION_ALREADY_ACTIVE', '该会话已有生成任务进行中', false, {
          http_status: 409,
        })
      }
      const history = state.messages.get(chatId) ?? []
      if (history.length === 0 || history[history.length - 1].role !== 'assistant') {
        throw new ApiError('INVALID_REQUEST', '没有可重新生成的回答', false)
      }
      state.activeGenerations.add(chatId)
      // The backend deletes the last assistant answer and regenerates it.
      state.messages.set(
        chatId,
        history.filter((_, index) => index !== history.length - 1),
      )
      const lastUser = [...(state.messages.get(chatId) ?? [])]
        .reverse()
        .find((message) => message.role === 'user')
      return runMockGeneration(
        chatId,
        buildMockReply(lastUser?.content ?? ''),
        { thinking: state.lastThinking.get(chatId) === true, forceError: false },
      )(handlers)
    },

    async compactChat(chatId: string) {
      await sleep(400)
      const count = (state.messages.get(chatId) ?? []).length
      return { summary: `Mock 摘要：会话共 ${count} 条消息，较早上下文已压缩为要点。` }
    },

    async exportBackup(_path: string) {
      await sleep(200)
      // Mock mode: nothing is written to disk.
    },

    async importBackup(_path: string) {
      await sleep(200)
    },

    async clearBusinessData() {
      state.cache.clear()
      state.history = []
      state.responses.clear()
      state.vocabulary.clear()
      state.tabs = []
      for (const chat of state.chats) {
        state.messages.set(chat.id, [])
        state.conversationContext.set(chat.id, '')
      }
    },

    async resetApp() {
      const fresh = createInitialState()
      // Reset also clears the credential, so onboarding shows again.
      fresh.provider = { ...defaultProvider(), api_key_configured: false, api_key_hint: '' }
      state.settings = fresh.settings
      state.provider = fresh.provider
      state.capabilities = fresh.capabilities
      state.cache = fresh.cache
      state.history = fresh.history
      state.responses = fresh.responses
      state.vocabulary = fresh.vocabulary
      state.terminology = fresh.terminology
      state.tabs = fresh.tabs
      state.chats = fresh.chats
      state.messages = fresh.messages
      state.conversationContext = fresh.conversationContext
      state.globalContext = fresh.globalContext
      state.activeGenerations = fresh.activeGenerations
      state.lastThinking = fresh.lastThinking
    },
  }

  return client
}
