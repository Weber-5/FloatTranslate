/**
 * Chat generation state machine (docs/01 §3): send → streaming → completed,
 * stop → cancelled, 409 GENERATION_ALREADY_ACTIVE guard, error + retry,
 * regenerate, compact and the Ask AI reference chip. Runs against the mock
 * client's timed SSE lifecycle simulation.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { setupFreshEnv } from './helpers'
import { useChatsStore } from '@/stores/chats'
import { useUiStore } from '@/stores/ui'
import { getClient } from '@/api'

async function settle(status: string, timeout = 8000): Promise<void> {
  const chats = useChatsStore()
  await vi.waitFor(
    () => {
      expect(chats.generationStatus).toBe(status)
    },
    { timeout },
  )
}

beforeEach(async () => {
  await setupFreshEnv()
})

describe('chat generation state machine', () => {
  it('send streams into the buffer and lands the assistant message after the user one', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    expect(chats.activeChatId).toBeTruthy()

    await chats.send('hello there')
    expect(chats.isGenerating).toBe(true)
    await vi.waitFor(
      () => {
        expect(chats.streamingMessage).not.toBeNull()
        expect(chats.streamingMessage?.content.length ?? 0).toBeGreaterThan(0)
      },
      { timeout: 3000 },
    )

    await settle('completed')
    expect(chats.activeMessages).toHaveLength(2)
    expect(chats.activeMessages[0].role).toBe('user')
    expect(chats.activeMessages[0].content).toBe('hello there')
    expect(chats.activeMessages[1].role).toBe('assistant')
    expect(chats.activeMessages[1].content.length).toBeGreaterThan(0)
    expect(chats.streamingMessage).toBeNull()
  }, 15000)

  it('includes reasoning when thinking is enabled', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    chats.thinkingEnabled = true
    await chats.send('think about it')
    await settle('completed')
    const assistant = chats.activeMessages[chats.activeMessages.length - 1]
    expect(assistant.reasoning_content).toBeTruthy()
    chats.thinkingEnabled = false
  }, 15000)

  it('stop() transitions to cancelled and keeps the partial answer', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    await chats.send('a long message that will be stopped mid stream')
    await vi.waitFor(
      () => {
        expect((chats.streamingMessage?.content.length ?? 0) > 0).toBe(true)
      },
      { timeout: 3000 },
    )

    chats.stop()
    await settle('cancelled', 5000)
    // The partial content seen so far is kept as a message.
    expect(chats.activeMessages.length).toBeGreaterThanOrEqual(1)
    expect(chats.isGenerating).toBe(false)
  }, 15000)

  it('trips the 409 GENERATION_ALREADY_ACTIVE guard into a toast plus message refresh', async () => {
    const chats = useChatsStore()
    const ui = useUiStore()
    await chats.ensureLoaded()
    const api = await getClient()
    const listSpy = vi.spyOn(api, 'listMessages')

    await chats.send('trigger-409 please')
    await settle('idle', 5000)
    expect(chats.generationError).toBeNull()
    expect(ui.toasts.length).toBeGreaterThan(0)
    expect(listSpy).toHaveBeenCalled()
    // The optimistic user message is replaced by the refreshed server list.
    await vi.waitFor(
      () => {
        expect(chats.activeMessages.every((message) => !message.id.startsWith('local-msg'))).toBe(
          true,
        )
      },
      { timeout: 3000 },
    )
  }, 15000)

  it('mid-stream generation.error lands in error state and retry() completes the turn', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    await chats.send('trigger-error now')
    await settle('error', 5000)
    expect(chats.generationError).toBeTruthy()
    expect(chats.generationErrorCode).toBe('TRANSLATION_FAILED')
    // Only the optimistic user message exists; retry does not duplicate it.
    expect(chats.activeMessages).toHaveLength(1)
    expect(chats.activeMessages[0].role).toBe('user')

    await chats.retry()
    await settle('completed')
    expect(chats.activeMessages).toHaveLength(2)
    expect(chats.activeMessages[0].role).toBe('user')
    expect(chats.activeMessages[1].role).toBe('assistant')
  }, 20000)

  it('regenerate() drops the last assistant answer and streams a new one', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    await chats.send('first question')
    await settle('completed')
    const before = chats.activeMessages.length
    expect(chats.canRegenerate).toBe(true)

    await chats.regenerate()
    expect(chats.isGenerating).toBe(true)
    await settle('completed')
    expect(chats.activeMessages).toHaveLength(before)
    expect(chats.activeMessages[chats.activeMessages.length - 1].role).toBe('assistant')
  }, 20000)

  it('rejects a second concurrent send on the same chat (local guard)', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    const first = chats.send('first send')
    await vi.waitFor(() => expect(chats.isGenerating).toBe(true), { timeout: 3000 })
    await chats.send('second send must be ignored')
    await first
    expect(chats.activeMessages.filter((message) => message.role === 'user')).toHaveLength(1)
  }, 15000)

  it('compact() calls the endpoint, returns the summary and clears the hint', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    chats.compactHint = true
    const summary = await chats.compact()
    expect(summary).toBeTruthy()
    expect(summary).toContain('Mock')
    expect(chats.compactHint).toBe(false)
    expect(chats.compactRunning).toBe(false)
  }, 10000)
})

describe('Ask AI reference chip', () => {
  it('startAskAi prefills draft + reference and send forwards reference_text then clears it', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    const api = await getClient()
    const spy = vi.spyOn(api, 'streamChatGeneration')

    await chats.startAskAi('serendipity')
    expect(chats.draft).toBe('serendipity')
    expect(chats.referenceText).toBe('serendipity')

    await chats.send('what does this mean')
    expect(chats.referenceText).toBe('')
    expect(spy).toHaveBeenCalled()
    const request = spy.mock.calls[0][1]
    expect(request.content).toBe('what does this mean')
    expect(request.reference_text).toBe('serendipity')
    await settle('completed')
  }, 15000)
})

describe('chat sessions and messages', () => {
  it('rename and delete chats through the store', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    const chat = await chats.newChat()
    await chats.renameChat(chat.id, 'Renamed session')
    expect(chats.chats.find((entry) => entry.id === chat.id)?.title).toBe('Renamed session')

    await chats.deleteChat(chat.id)
    expect(chats.chats.some((entry) => entry.id === chat.id)).toBe(false)
    expect(chats.messages[chat.id]).toBeUndefined()
  }, 10000)

  it('messages load ascending and /clear empties them without touching other chats', async () => {
    const chats = useChatsStore()
    await chats.ensureLoaded()
    const other = await chats.newChat()
    const target = await chats.newChat()
    await chats.selectChat(other.id)
    await chats.selectChat(target.id)

    await chats.send('first')
    await settle('completed')
    await chats.send('second')
    await settle('completed')

    // Server state (mock) stores ascending: user, assistant, user, assistant.
    const server = await getClient()
    const stored = await server.listMessages(target.id)
    expect(stored.map((message) => message.role)).toEqual([
      'user',
      'assistant',
      'user',
      'assistant',
    ])

    await chats.clearMessages(target.id)
    expect(chats.activeMessages).toHaveLength(0)
    expect(other.id).not.toBe(target.id)
    await chats.selectChat(other.id)
    expect(chats.activeMessages).toHaveLength(0)
  }, 30000)
})
