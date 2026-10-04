/**
 * AI Sidebar store: sessions, messages and the chat generation state machine
 * (docs/01 §3): idle → preparing_context → streaming → completed | cancelled
 * | error. Only one active generation per session is allowed — the backend
 * enforces it with 409 GENERATION_ALREADY_ACTIVE, the store mirrors the guard
 * locally (toast + message refresh when it still trips).
 *
 * Streaming uses two dedicated reactive buffers (content / reasoning) that
 * are only rendered inside the streaming bubble, so delta appends never
 * re-render the whole message list.
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { i18n } from '@/i18n'
import type { Chat, ChatMessage } from '@/api/types'
import { useApi, toApiError, type ApiError, type ChatGenerationHandle, type ChatGenerationHandlers } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import { useUiStore } from '@/stores/ui'

export type GenerationStatus = 'idle' | 'preparing_context' | 'streaming' | 'completed' | 'cancelled' | 'error'

/** Thrown by the backend when a second generation hits the same chat. */
const GENERATION_ALREADY_ACTIVE = 'GENERATION_ALREADY_ACTIVE'

let localMessageCounter = 0
function localMessageId(): string {
  localMessageCounter += 1
  return `local-msg-${localMessageCounter}`
}

export const useChatsStore = defineStore('chats', () => {
  const chats = ref<Chat[]>([])
  const activeChatId = ref<string | null>(null)
  const messages = ref<Record<string, ChatMessage[]>>({})
  const loaded = ref(false)

  const generationStatus = ref<GenerationStatus>('idle')
  const generationError = ref<string | null>(null)
  const generationErrorCode = ref<string | null>(null)
  const thinkingEnabled = ref(false)
  /** Composer draft, shared with the sidebar so Ask AI can prefill it. */
  const draft = ref('')
  /** Ask AI reference text (docs/06 §12); shown as a chip, cleared on send. */
  const referenceText = ref('')

  /** Manual /compact spinner state. */
  const compactRunning = ref(false)
  /** Optimistic "context is large, consider /compact" hint. */
  const compactHint = ref(false)

  // Dedicated streaming buffers — plain string refs, appended in place.
  const streamingActive = ref(false)
  const streamingContent = ref('')
  const streamingReasoning = ref('')
  let streamingStartedAt = ''
  const streamingMessage = computed<ChatMessage | null>(() => {
    if (!streamingActive.value) return null
    return {
      id: 'streaming',
      role: 'assistant',
      content: streamingContent.value,
      ...(streamingReasoning.value.length > 0
        ? { reasoning_content: streamingReasoning.value }
        : {}),
      created_at: streamingStartedAt,
    }
  })

  /** Conversation contexts seen by the editor, used for the size estimate. */
  const conversationContextCache = new Map<string, string>()

  const activeChat = computed<Chat | null>(
    () => chats.value.find((chat) => chat.id === activeChatId.value) ?? null,
  )

  const activeMessages = computed<ChatMessage[]>(() =>
    activeChatId.value ? (messages.value[activeChatId.value] ?? []) : [],
  )

  const isGenerating = computed(
    () => generationStatus.value === 'preparing_context' || generationStatus.value === 'streaming',
  )

  /** Regenerate is offered on the last message when it is an assistant reply. */
  const lastMessage = computed<ChatMessage | null>(
    () => activeMessages.value[activeMessages.value.length - 1] ?? null,
  )
  const canRegenerate = computed(
    () => !isGenerating.value && lastMessage.value?.role === 'assistant',
  )

  let activeHandle: ChatGenerationHandle | null = null
  let stopGuard: ReturnType<typeof setTimeout> | null = null

  function clearStopGuard(): void {
    if (stopGuard) {
      clearTimeout(stopGuard)
      stopGuard = null
    }
  }

  function appendMessage(chatId: string, message: ChatMessage): void {
    messages.value = {
      ...messages.value,
      [chatId]: [...(messages.value[chatId] ?? []), message],
    }
  }

  async function ensureLoaded(): Promise<void> {
    if (loaded.value) return
    chats.value = await useApi().listChats()
    loaded.value = true
    if (chats.value.length > 0) {
      await selectChat(chats.value[0].id)
    }
  }

  async function loadMessages(chatId: string): Promise<void> {
    if (messages.value[chatId]) return
    await reloadMessages(chatId)
  }

  /** Force a fresh GET (ascending order per contract) — used after 409 etc. */
  async function reloadMessages(chatId: string): Promise<void> {
    messages.value = { ...messages.value, [chatId]: await useApi().listMessages(chatId) }
  }

  async function selectChat(id: string): Promise<void> {
    activeChatId.value = id
    compactHint.value = false
    await loadMessages(id)
  }

  async function newChat(): Promise<Chat> {
    const chat = await useApi().createChat()
    chats.value = [chat, ...chats.value]
    messages.value = { ...messages.value, [chat.id]: [] }
    activeChatId.value = chat.id
    compactHint.value = false
    return chat
  }

  async function renameChat(id: string, title: string): Promise<void> {
    await useApi().renameChat(id, title)
    chats.value = chats.value.map((chat) => (chat.id === id ? { ...chat, title } : chat))
  }

  async function deleteChat(id: string): Promise<void> {
    if (id === activeChatId.value && isGenerating.value) stop()
    await useApi().deleteChat(id)
    conversationContextCache.delete(id)
    chats.value = chats.value.filter((chat) => chat.id !== id)
    const nextMessages: Record<string, ChatMessage[]> = { ...messages.value }
    delete nextMessages[id]
    messages.value = nextMessages
    if (activeChatId.value === id) {
      activeChatId.value = chats.value[0]?.id ?? null
      if (activeChatId.value) await loadMessages(activeChatId.value)
    }
  }

  /** `/clear`: immediately delete all messages (context is kept server-side). */
  async function clearMessages(id: string): Promise<void> {
    await useApi().clearChatMessages(id)
    messages.value = { ...messages.value, [id]: [] }
    if (isGenerating.value) stop()
    clearStopGuard()
    streamingActive.value = false
    activeHandle = null
    generationStatus.value = 'idle'
    generationError.value = null
    generationErrorCode.value = null
    compactHint.value = false
  }

  async function ensureChat(): Promise<Chat> {
    if (activeChat.value) return activeChat.value
    if (chats.value.length > 0) {
      await selectChat(chats.value[0].id)
      return chats.value[0]
    }
    return newChat()
  }

  // ---- Context size estimate (compact hint, optimistic only) -------------

  function estimateContextTokens(chatId: string): number {
    let chars = 0
    for (const message of messages.value[chatId] ?? []) {
      chars += message.content.length + (message.reasoning_content?.length ?? 0)
    }
    chars += useSettingsStore().globalContext.length
    chars += conversationContextCache.get(chatId)?.length ?? 0
    return Math.ceil(chars / 4)
  }

  /**
   * Freeze decision (Phase 4): the backend is the compact authority and
   * auto-compacts on its own. The frontend only ESTIMATES (chars/4) and
   * shows a hint — it never POSTs compact by itself.
   */
  function evaluateCompactHint(chatId: string): void {
    const settings = useSettingsStore()
    const effective =
      settings.capabilities?.effective_context_tokens ??
      settings.capabilities?.configured_context_tokens ??
      settings.app?.context_tokens ??
      1000000
    const threshold = (settings.app?.compact_threshold ?? 80) / 100
    compactHint.value = estimateContextTokens(chatId) > effective * threshold
  }

  // ---- Generation state machine -------------------------------------------

  function buildHandlers(chatId: string): ChatGenerationHandlers {
    return {
      onStarted: () => {
        generationStatus.value = 'streaming'
      },
      onReasoningDelta: (delta) => {
        streamingReasoning.value += delta
      },
      onContentDelta: (delta) => {
        streamingContent.value += delta
      },
      onCompleted: (_generationId, data) => {
        const reasoning = data?.reasoning_content ?? streamingReasoning.value
        appendMessage(chatId, {
          id: data?.message_id ?? localMessageId(),
          role: 'assistant',
          content: data?.content ?? streamingContent.value,
          ...(reasoning.length > 0 ? { reasoning_content: reasoning } : {}),
          created_at: new Date().toISOString(),
        })
        clearStopGuard()
        streamingActive.value = false
        activeHandle = null
        generationStatus.value = 'completed'
        void evaluateCompactHint(chatId)
      },
      onCancelled: () => {
        // Keep whatever partial answer already streamed (backend does too).
        if (streamingContent.value.length > 0) {
          appendMessage(chatId, {
            id: localMessageId(),
            role: 'assistant',
            content: streamingContent.value,
            ...(streamingReasoning.value.length > 0
              ? { reasoning_content: streamingReasoning.value }
              : {}),
            created_at: new Date().toISOString(),
          })
        }
        clearStopGuard()
        streamingActive.value = false
        activeHandle = null
        generationStatus.value = 'cancelled'
      },
      onError: (error: ApiError) => {
        clearStopGuard()
        streamingActive.value = false
        activeHandle = null
        if (error.code === GENERATION_ALREADY_ACTIVE) {
          // Guard tripped server-side: surface a toast and resync with the
          // backend's authoritative message list.
          generationStatus.value = 'idle'
          generationError.value = null
          generationErrorCode.value = null
          useUiStore().showToast(t409Message(error), 'error')
          void reloadMessages(chatId).catch(() => undefined)
        } else {
          generationError.value = error.message
          generationErrorCode.value = error.code
          generationStatus.value = 'error'
        }
      },
    }
  }

  function t409Message(error: ApiError): string {
    // Prefer the localized catalog; fall back to the server message.
    const localized = i18n.global.t('ai.generationActive')
    return localized !== 'ai.generationActive' ? localized : error.message
  }

  async function runGeneration(
    chatId: string,
    mode: 'send' | 'regenerate',
    payload?: { content: string; referenceText?: string; appendUser?: boolean },
  ): Promise<void> {
    generationStatus.value = 'preparing_context'
    generationError.value = null
    generationErrorCode.value = null
    compactHint.value = false
    streamingContent.value = ''
    streamingReasoning.value = ''
    streamingStartedAt = new Date().toISOString()
    streamingActive.value = true

    if (mode === 'send' && payload?.appendUser !== false) {
      appendMessage(chatId, {
        id: localMessageId(),
        role: 'user',
        content: payload!.content,
        created_at: new Date().toISOString(),
      })
    }

    const handlers = buildHandlers(chatId)
    try {
      const api = useApi()
      activeHandle =
        mode === 'send'
          ? await api.streamChatGeneration(
              chatId,
              {
                content: payload!.content,
                thinking: thinkingEnabled.value,
                ...(payload?.referenceText && payload.referenceText.trim().length > 0
                  ? { reference_text: payload.referenceText }
                  : {}),
              },
              handlers,
            )
          : await api.regenerateChat(chatId, handlers)
    } catch (err) {
      // HTTP-level failures (409 guard, backend down) land here.
      handlers.onError?.(toApiError(err))
    }
  }

  async function send(content: string, opts?: { referenceText?: string }): Promise<void> {
    const trimmed = content.trim()
    if (trimmed.length === 0) return
    if (isGenerating.value) return // one active generation per session (docs/01 §3)

    // The Ask AI reference chip (docs/06 §12) travels with the send that
    // clears it — either the chip the sidebar shows or an explicit opt.
    const reference = opts?.referenceText ?? referenceText.value
    referenceText.value = ''
    const chat = await ensureChat()
    await runGeneration(chat.id, 'send', {
      content: trimmed,
      referenceText: reference,
    })
  }

  function stop(): void {
    if (!isGenerating.value) return
    const handle = activeHandle
    handle?.cancel()
    // Safety net: if the stream never delivers generation.cancelled (host
    // gone / transport dead), settle the state machine locally.
    clearStopGuard()
    stopGuard = setTimeout(() => {
      if (isGenerating.value) {
        streamingActive.value = false
        activeHandle = null
        generationStatus.value = 'cancelled'
      }
    }, 3000)
    void handle
  }

  /** Regenerates the last assistant answer (docs/04 §11). */
  async function regenerate(): Promise<void> {
    if (isGenerating.value) return
    const chat = activeChat.value
    if (!chat) return
    const list = messages.value[chat.id] ?? []
    if (list.length === 0 || list[list.length - 1].role !== 'assistant') return
    // Drop the last assistant reply locally; the backend does the same
    // before streaming the replacement.
    messages.value = { ...messages.value, [chat.id]: list.slice(0, -1) }
    await runGeneration(chat.id, 'regenerate')
  }

  /**
   * Retry after a generation error: regenerate when a (partial) assistant
   * reply exists, otherwise re-stream the failed user turn without
   * duplicating the optimistic user message.
   */
  async function retry(): Promise<void> {
    if (isGenerating.value) return
    const chat = activeChat.value
    if (!chat) return
    const list = messages.value[chat.id] ?? []
    if (list.length > 0 && list[list.length - 1].role === 'assistant') {
      await regenerate()
      return
    }
    const lastUser = [...list].reverse().find((message) => message.role === 'user')
    if (!lastUser) return
    await runGeneration(chat.id, 'send', { content: lastUser.content, appendUser: false })
  }

  /** Manual /compact (docs/04 §12). Returns the summary, null on failure. */
  async function compact(): Promise<string | null> {
    if (compactRunning.value) return null
    const chat = await ensureChat()
    compactRunning.value = true
    try {
      const { summary } = await useApi().compactChat(chat.id)
      compactHint.value = false
      return summary
    } catch (err) {
      useUiStore().showToast(toApiError(err).message, 'error')
      return null
    } finally {
      compactRunning.value = false
    }
  }

  async function askAi(referenceText: string): Promise<void> {
    await send(referenceText, { referenceText })
  }

  /**
   * Ask AI (docs/06 §12): create a NEW chat, open the sidebar and prefill
   * the composer with the word/text. The text also becomes the request's
   * reference_text when the user sends, displayed as a chip until then.
   */
  async function startAskAi(reference: string): Promise<void> {
    const content = reference.trim()
    if (content.length === 0) return
    await newChat()
    draft.value = content
    referenceText.value = content
  }

  async function saveConversationContext(content: string): Promise<void> {
    const chat = await ensureChat()
    await useApi().putConversationContext(chat.id, { content })
    conversationContextCache.set(chat.id, content)
  }

  async function loadConversationContext(): Promise<string> {
    const chat = await ensureChat()
    const content = (await useApi().getConversationContext(chat.id)).content
    conversationContextCache.set(chat.id, content)
    return content
  }

  /** After data reset / clear-business the cached view state is dropped. */
  function resetLocalState(): void {
    clearStopGuard()
    chats.value = []
    activeChatId.value = null
    messages.value = {}
    loaded.value = false
    generationStatus.value = 'idle'
    generationError.value = null
    generationErrorCode.value = null
    streamingActive.value = false
    streamingContent.value = ''
    streamingReasoning.value = ''
    draft.value = ''
    referenceText.value = ''
    compactHint.value = false
    compactRunning.value = false
    conversationContextCache.clear()
    activeHandle = null
  }

  return {
    chats,
    activeChatId,
    messages,
    loaded,
    generationStatus,
    generationError,
    generationErrorCode,
    thinkingEnabled,
    draft,
    referenceText,
    compactRunning,
    compactHint,
    streamingMessage,
    activeChat,
    activeMessages,
    isGenerating,
    lastMessage,
    canRegenerate,
    ensureLoaded,
    selectChat,
    newChat,
    renameChat,
    deleteChat,
    clearMessages,
    loadMessages,
    reloadMessages,
    send,
    stop,
    regenerate,
    retry,
    compact,
    askAi,
    startAskAi,
    saveConversationContext,
    loadConversationContext,
    resetLocalState,
  }
})
