/**
 * AI Sidebar store: sessions, messages and the chat generation state machine
 * (docs/01 §3): idle → preparing_context → streaming → completed | cancelled
 * | error. Only one active generation per session is allowed.
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import type { Chat, ChatMessage } from '@/api/types'
import { useApi, toApiError, type ChatGenerationHandle } from '@/api'

export type GenerationStatus = 'idle' | 'preparing_context' | 'streaming' | 'completed' | 'cancelled' | 'error'

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
  const streamingMessage = ref<ChatMessage | null>(null)
  const thinkingEnabled = ref(false)
  /** Composer draft, shared with the sidebar so Ask AI can prefill it. */
  const draft = ref('')

  const activeChat = computed<Chat | null>(
    () => chats.value.find((chat) => chat.id === activeChatId.value) ?? null,
  )

  const activeMessages = computed<ChatMessage[]>(() =>
    activeChatId.value ? (messages.value[activeChatId.value] ?? []) : [],
  )

  const isGenerating = computed(
    () => generationStatus.value === 'preparing_context' || generationStatus.value === 'streaming',
  )

  let activeHandle: ChatGenerationHandle | null = null

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
    messages.value = { ...messages.value, [chatId]: await useApi().listMessages(chatId) }
  }

  async function selectChat(id: string): Promise<void> {
    activeChatId.value = id
    await loadMessages(id)
  }

  async function newChat(): Promise<Chat> {
    const chat = await useApi().createChat()
    chats.value = [chat, ...chats.value]
    messages.value = { ...messages.value, [chat.id]: [] }
    activeChatId.value = chat.id
    return chat
  }

  async function renameChat(id: string, title: string): Promise<void> {
    await useApi().renameChat(id, title)
    chats.value = chats.value.map((chat) => (chat.id === id ? { ...chat, title } : chat))
  }

  async function deleteChat(id: string): Promise<void> {
    await useApi().deleteChat(id)
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
    if (generationStatus.value === 'streaming' || generationStatus.value === 'preparing_context') {
      stop()
    }
    generationStatus.value = 'idle'
    generationError.value = null
  }

  async function ensureChat(): Promise<Chat> {
    if (activeChat.value) return activeChat.value
    if (chats.value.length > 0) {
      await selectChat(chats.value[0].id)
      return chats.value[0]
    }
    return newChat()
  }

  async function send(content: string, opts?: { referenceText?: string }): Promise<void> {
    const trimmed = content.trim()
    if (trimmed.length === 0) return
    if (isGenerating.value) return // one active generation per session (docs/01 §3)

    const chat = await ensureChat()
    const userMessage: ChatMessage = {
      id: localMessageId(),
      role: 'user',
      content: trimmed,
      created_at: new Date().toISOString(),
    }
    messages.value = {
      ...messages.value,
      [chat.id]: [...(messages.value[chat.id] ?? []), userMessage],
    }

    generationStatus.value = 'preparing_context'
    generationError.value = null
    streamingMessage.value = {
      id: localMessageId(),
      role: 'assistant',
      content: '',
      reasoning_content: '',
      created_at: new Date().toISOString(),
    }

    try {
      const handle = await useApi().streamChatGeneration(
        chat.id,
        { content: trimmed, thinking: thinkingEnabled.value, reference_text: opts?.referenceText },
        {
          onStarted: () => {
            generationStatus.value = 'streaming'
          },
          onReasoningDelta: (delta) => {
            if (streamingMessage.value) {
              streamingMessage.value = {
                ...streamingMessage.value,
                reasoning_content: (streamingMessage.value.reasoning_content ?? '') + delta,
              }
            }
          },
          onContentDelta: (delta) => {
            if (streamingMessage.value) {
              streamingMessage.value = {
                ...streamingMessage.value,
                content: streamingMessage.value.content + delta,
              }
            }
          },
          onCompleted: () => {
            if (streamingMessage.value) {
              messages.value = {
                ...messages.value,
                [chat.id]: [...(messages.value[chat.id] ?? []), streamingMessage.value],
              }
            }
            streamingMessage.value = null
            activeHandle = null
            generationStatus.value = 'completed'
          },
          onCancelled: () => {
            if (streamingMessage.value && streamingMessage.value.content.length > 0) {
              messages.value = {
                ...messages.value,
                [chat.id]: [...(messages.value[chat.id] ?? []), streamingMessage.value],
              }
            }
            streamingMessage.value = null
            activeHandle = null
            generationStatus.value = 'cancelled'
          },
          onError: (error) => {
            streamingMessage.value = null
            activeHandle = null
            generationError.value = error.message
            generationStatus.value = 'error'
          },
        },
      )
      activeHandle = handle
    } catch (err) {
      const apiError = toApiError(err)
      streamingMessage.value = null
      activeHandle = null
      generationError.value = apiError.message
      generationStatus.value = 'error'
    }
  }

  function stop(): void {
    activeHandle?.cancel()
  }

  async function askAi(referenceText: string): Promise<void> {
    await send(referenceText, { referenceText })
  }

  /**
   * Ask AI (Phase 2 contract, docs/06 §12): create a NEW chat, open the
   * sidebar and prefill the composer with the word/text as a reference.
   * Sending stays user-triggered (real streaming arrives in Phase 4).
   */
  async function startAskAi(referenceText: string): Promise<void> {
    const content = referenceText.trim()
    if (content.length === 0) return
    await newChat()
    draft.value = content
  }

  async function saveConversationContext(content: string): Promise<void> {
    const chat = await ensureChat()
    await useApi().putConversationContext(chat.id, { content })
  }

  async function loadConversationContext(): Promise<string> {
    const chat = await ensureChat()
    return (await useApi().getConversationContext(chat.id)).content
  }

  /** After data reset / clear-business the cached view state is dropped. */
  function resetLocalState(): void {
    chats.value = []
    activeChatId.value = null
    messages.value = {}
    loaded.value = false
    generationStatus.value = 'idle'
    generationError.value = null
    streamingMessage.value = null
    draft.value = ''
    activeHandle = null
  }

  return {
    chats,
    activeChatId,
    messages,
    loaded,
    generationStatus,
    generationError,
    streamingMessage,
    thinkingEnabled,
    draft,
    activeChat,
    activeMessages,
    isGenerating,
    ensureLoaded,
    selectChat,
    newChat,
    renameChat,
    deleteChat,
    clearMessages,
    send,
    stop,
    askAi,
    startAskAi,
    saveConversationContext,
    loadConversationContext,
    resetLocalState,
  }
})
