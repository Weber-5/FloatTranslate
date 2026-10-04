/**
 * AI Sidebar Phase 4 UI (docs/03 §9): /context modal, /export via the Tauri
 * host contract with blob fallback, regenerate on the last assistant
 * message, reasoning collapsed by default, reference chip, rename/delete.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { setupFreshEnv } from './helpers'
import { useChatsStore } from '@/stores/chats'
import { useUiStore } from '@/stores/ui'
import { __setExportInvokeForTests, composeChatMarkdown } from '@/services/export'
import AiSidebar from '@/components/layout/AiSidebar.vue'

async function mountSidebar() {
  const { i18n } = await setupFreshEnv()
  const chats = useChatsStore()
  await chats.ensureLoaded()
  // attachTo: ConfirmModal / context modal teleport to document.body.
  const wrapper = mount(AiSidebar, {
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
  await wrapper.vm.$nextTick()
  return { wrapper, chats, i18n }
}

function modalButton(selector: string): HTMLButtonElement {
  const button = document.querySelector(selector)
  expect(button).not.toBeNull()
  return button as HTMLButtonElement
}

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

afterEach(() => {
  __setExportInvokeForTests(null)
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('AI sidebar Phase 4 UI', () => {
  it('/context opens the modal editor, 保存 persists and closes it', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    const api = await (await import('@/api')).getClient()
    const putSpy = vi.spyOn(api, 'putConversationContext')

    const composer = wrapper.find('[data-testid="ai-composer"]')
    await composer.setValue('/context')
    await composer.trigger('keydown', { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve, 10))

    const modal = document.querySelector('[data-testid="context-modal"]')
    expect(modal).not.toBeNull()
    const textarea = document.querySelector('[data-testid="context-textarea"]') as HTMLTextAreaElement
    expect(textarea).not.toBeNull()

    textarea.value = '偏好：正式语气'
    textarea.dispatchEvent(new Event('input'))
    ;(document.querySelector('[data-testid="context-save"]') as HTMLButtonElement).click()
    await new Promise((resolve) => setTimeout(resolve, 10))

    expect(putSpy).toHaveBeenCalledWith(chats.activeChatId, { content: '偏好：正式语气' })
    await wrapper.vm.$nextTick()
    expect(document.querySelector('[data-testid="context-modal"]')).toBeNull()
  })

  it('/export hands the composed markdown to the Tauri host and toasts the path', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    const ui = useUiStore()

    await chats.send('export me')
    await settle('completed')

    const invoke = vi.fn(async (_cmd: string, args?: Record<string, unknown>) => {
      expect(String(args?.default_file_name).endsWith('.md')).toBe(true)
      expect(String(args?.content)).toContain('# ')
      return { ok: true, path: 'C:\\Exports\\chat.md' }
    })
    __setExportInvokeForTests(invoke)

    const composer = wrapper.find('[data-testid="ai-composer"]')
    await composer.setValue('/export')
    await composer.trigger('keydown', { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve, 10))

    expect(invoke).toHaveBeenCalledWith('export_markdown', expect.anything())
    await vi.waitFor(() => {
      expect(ui.toasts.some((toast) => toast.message.includes('C:\\Exports\\chat.md'))).toBe(true)
    })
  }, 15000)

  it('/export falls back to a blob download when the host command is missing', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    await chats.send('fallback export')
    await settle('completed')

    // jsdom has no Blob-URL API — install test doubles first.
    const createObjectURL = vi.fn(() => 'blob:mock')
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'createObjectURL', {
      value: createObjectURL,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: revokeObjectURL,
      configurable: true,
      writable: true,
    })

    const composer = wrapper.find('[data-testid="ai-composer"]')
    await composer.setValue('/export')
    await composer.trigger('keydown', { key: 'Enter' })
    await new Promise((resolve) => setTimeout(resolve, 10))

    expect(createObjectURL).toHaveBeenCalled()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock')
  }, 15000)

  it('shows the regenerate button on the last assistant message and regenerates', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()

    expect(wrapper.find('[data-testid="ai-regenerate"]').exists()).toBe(false)
    await chats.send('regenerate target')
    await settle('completed')
    await wrapper.vm.$nextTick()

    const button = wrapper.find('[data-testid="ai-regenerate"]')
    expect(button.exists()).toBe(true)
    await button.trigger('click')
    expect(chats.isGenerating).toBe(true)
    await settle('completed')
    await wrapper.vm.$nextTick()
    // Still exactly one user + one assistant pair after regeneration.
    expect(chats.activeMessages).toHaveLength(2)
  }, 20000)

  it('renders reasoning collapsed by default (details without open)', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    chats.thinkingEnabled = true
    await chats.send('reasoning visibility')
    await settle('completed')
    chats.thinkingEnabled = false
    await wrapper.vm.$nextTick()

    const details = wrapper.find('details.reasoning')
    expect(details.exists()).toBe(true)
    expect(details.attributes('open')).toBeUndefined()
    expect(details.find('summary').text()).toBe('思考过程')
  }, 15000)

  it('thinking toggle defaults to off and stays chat-local', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    expect(chats.thinkingEnabled).toBe(false)
    const toggle = wrapper.find('[data-testid="thinking-toggle"]')
    expect(toggle.exists()).toBe(true)
    await toggle.trigger('click')
    expect(chats.thinkingEnabled).toBe(true)
    await toggle.trigger('click')
    expect(chats.thinkingEnabled).toBe(false)
  })

  it('shows the Ask AI reference chip and clears it on send', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    await chats.startAskAi('ephemeral')
    await wrapper.vm.$nextTick()

    const chip = wrapper.find('[data-testid="reference-chip"]')
    expect(chip.exists()).toBe(true)
    expect(chip.text()).toContain('ephemeral')

    const composer = wrapper.find('[data-testid="ai-composer"]')
    await composer.setValue('explain please')
    await composer.trigger('keydown', { key: 'Enter' })
    expect(chats.referenceText).toBe('')
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-testid="reference-chip"]').exists()).toBe(false)
    await settle('completed')
  }, 15000)

  it('renames a session inline and deletes one through the confirm modal', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    await chats.newChat()
    await wrapper.vm.$nextTick()

    await wrapper.find('[data-testid="session-switcher"]').trigger('click')
    const items = wrapper.findAll('.session-item')
    const target = items.filter((item) => item.text().includes('新会话')).at(-1)
    expect(target).toBeTruthy()

    await target!.find('button[aria-label="重命名会话"]').trigger('click')
    const input = wrapper.find('.session-rename-input')
    await input.setValue('工作会话')
    await input.trigger('keydown.enter')
    await new Promise((resolve) => setTimeout(resolve, 10))
    // Both initial sessions share the default title, so assert on the title
    // itself rather than the specific id picked in the menu.
    const renamedId = chats.chats.find((chat) => chat.title === '工作会话')?.id
    expect(renamedId).toBeTruthy()

    // The menu is still open after the inline rename — use it directly.
    const items2 = wrapper.findAll('.session-item')
    const target2 = items2.filter((item) => item.text().includes('工作会话')).at(-1)
    expect(target2).toBeTruthy()
    await target2!.find('button[aria-label="删除会话"]').trigger('click')
    await wrapper.vm.$nextTick()
    // ConfirmModal teleports to body.
    expect(document.querySelector('.modal-card')).not.toBeNull()
    modalButton('.modal-card .btn-danger').click()
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(chats.chats.some((chat) => chat.id === renamedId)).toBe(false)
    // The other sessions survive.
    expect(chats.chats.length).toBeGreaterThan(0)
  })

  it('shows the compact hint banner and runs /compact from it', async () => {
    const { wrapper } = await mountSidebar()
    const chats = useChatsStore()
    const ui = useUiStore()
    chats.compactHint = true
    await wrapper.vm.$nextTick()

    const hint = wrapper.find('[data-testid="compact-hint"]')
    expect(hint.exists()).toBe(true)
    expect(hint.text()).toContain('/compact')

    await wrapper.find('.banner-action').trigger('click')
    // compact() awaits the endpoint (mock sleeps 400ms) before clearing.
    await vi.waitFor(() => {
      expect(chats.compactHint).toBe(false)
    })
    await vi.waitFor(() => {
      expect(ui.toasts.some((toast) => toast.tone === 'success')).toBe(true)
    })
  })

  it('composeChatMarkdown uses i18n role labels and includes the exported_at header', () => {
    const chat = { id: 'c1', title: '标题', created_at: '', updated_at: '' }
    const markdown = composeChatMarkdown(
      chat,
      [
        { id: 'm1', role: 'user', content: 'hi', created_at: '' },
        { id: 'm2', role: 'assistant', content: '**hello**', reasoning_content: 'secret', created_at: '' },
      ],
      '用户',
      '助手',
      '导出于',
      '2026-10-04 10:00',
    )
    expect(markdown).toContain('# 标题')
    expect(markdown).toContain('导出于 2026-10-04 10:00')
    expect(markdown).toContain('**用户**：')
    expect(markdown).toContain('**助手**：')
    expect(markdown).toContain('**hello**')
    // Freeze: reasoning is NOT exported.
    expect(markdown).not.toContain('secret')
  })
})
