import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupTestEnv } from './helpers'
import { useChatsStore, type GenerationStatus } from '@/stores/chats'
import { getClient } from '@/api'
import AiSidebar from '@/components/layout/AiSidebar.vue'

async function mountSidebar() {
  const { i18n } = await setupTestEnv()
  const chats = useChatsStore()
  await chats.ensureLoaded()
  const wrapper = mount(AiSidebar, { global: { plugins: [i18n] } })
  await wrapper.vm.$nextTick()
  return { wrapper, chats }
}

describe('AI sidebar slash commands and generation controls', () => {
  beforeEach(async () => {
    await setupTestEnv()
  })

  it('opens the slash command menu when the composer starts with "/"', async () => {
    const { wrapper } = await mountSidebar()

    expect(wrapper.find('[data-testid="slash-menu"]').exists()).toBe(false)

    const composer = wrapper.find('[data-testid="ai-composer"]')
    await composer.setValue('/')
    expect(wrapper.find('[data-testid="slash-menu"]').exists()).toBe(true)
    for (const command of ['compact', 'clear', 'context', 'export']) {
      expect(wrapper.find(`[data-testid="slash-${command}"]`).exists()).toBe(true)
    }

    // Filter as more characters are typed.
    await composer.setValue('/cl')
    expect(wrapper.find('[data-testid="slash-clear"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="slash-compact"]').exists()).toBe(false)
  })

  it('executes /clear by calling clearChatMessages on the client and empties messages', async () => {
    const { wrapper, chats } = await mountSidebar()
    const chatId = chats.activeChatId
    expect(chatId).toBeTruthy()

    const api = await getClient()
    const spy = vi.spyOn(api, 'clearChatMessages')

    const composer = wrapper.find('[data-testid="ai-composer"]')
    await composer.setValue('/')
    await wrapper.find('[data-testid="slash-clear"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 10))

    expect(spy).toHaveBeenCalledWith(chatId)
    expect(chats.activeMessages).toHaveLength(0)
    expect((composer.element as HTMLTextAreaElement).value).toBe('')
  })

  it('shows the stop button while a generation is streaming, send otherwise', async () => {
    const { wrapper, chats } = await mountSidebar()

    expect(wrapper.find('[data-testid="ai-stop"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ai-send"]').exists()).toBe(true)

    chats.generationStatus = 'streaming' as GenerationStatus
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="ai-stop"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="ai-send"]').exists()).toBe(false)

    chats.generationStatus = 'idle' as GenerationStatus
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="ai-send"]').exists()).toBe(true)
  })

  it('sends a message on Enter and runs a mock generation to completion', async () => {
    const { wrapper, chats } = await mountSidebar()
    const composer = wrapper.find('[data-testid="ai-composer"]')
    await composer.setValue('explain the word suspend')
    // trigger('keydown.enter') does not set event.key; pass it explicitly.
    await composer.trigger('keydown', { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve, 60))
    expect(chats.isGenerating).toBe(true)

    // Wait for the mock stream to finish (~2s worst case).
    await vi.waitFor(
      () => {
        expect(chats.generationStatus).toBe('completed')
      },
      { timeout: 5000 },
    )
    expect(chats.activeMessages.length).toBe(2)
    expect(chats.activeMessages[0].role).toBe('user')
    expect(chats.activeMessages[1].role).toBe('assistant')
    expect(chats.activeMessages[1].content.length).toBeGreaterThan(0)
  }, 10000)
})
