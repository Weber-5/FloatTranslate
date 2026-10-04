import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { DOMWrapper } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { setupFreshEnv } from './helpers'
import { getClient } from '@/api'
import { useSettingsStore } from '@/stores/settings'
import { __setNativeInvokeForTests } from '@/services/native'
import DataSection from '@/components/settings/DataSection.vue'

type NativeCall = { cmd: string; args?: Record<string, unknown> }

let nativeCalls: NativeCall[] = []

function fakePicker(path: string | null): void {
  nativeCalls = []
  __setNativeInvokeForTests(async (cmd, args) => {
    nativeCalls.push({ cmd, args })
    if (cmd === 'pick_save_path' || cmd === 'pick_open_path') {
      return path === null ? { ok: false, cancelled: true } : { ok: true, path }
    }
    return undefined
  })
}

async function mountSection(): Promise<{
  wrapper: VueWrapper
  router: ReturnType<typeof createRouter>
  body: DOMWrapper<Element>
}> {
  const { i18n } = await setupFreshEnv()
  const settings = useSettingsStore()
  await settings.load()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/onboarding', name: 'onboarding', component: { template: '<div />' } },
      { path: '/translate', name: 'translate', component: { template: '<div />' } },
      { path: '/settings', name: 'settings', component: { template: '<div />' } },
      { path: '/', redirect: '/translate' },
    ],
  })
  await router.push('/settings')
  await router.isReady()
  const wrapper = mount(DataSection, {
    global: { plugins: [i18n, router] },
    attachTo: document.body,
  })
  await flushPromises()
  return { wrapper, router, body: new DOMWrapper(document.body) }
}

function lastModalConfirm(body: DOMWrapper<Element>): DOMWrapper<Element> {
  const buttons = body.findAll('.modal-actions button')
  return buttons[buttons.length - 1]
}

afterEach(() => {
  __setNativeInvokeForTests(null)
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('DataSection backup export (Phase 5)', () => {
  it('picks a save path, calls exportBackup with it, and shows the path', async () => {
    fakePicker('C:\\backups\\floattranslate-backup-x.json')
    const { wrapper, body } = await mountSection()
    const api = await getClient()
    const exportSpy = vi
      .spyOn(api, 'exportBackup')
      .mockResolvedValue({ exported: true, path: 'C:\\backups\\floattranslate-backup-x.json' })

    await wrapper.find('[data-testid="backup-export"]').trigger('click')
    await flushPromises()

    expect(exportSpy).toHaveBeenCalledTimes(1)
    expect(exportSpy.mock.calls[0][0]).toBe('C:\\backups\\floattranslate-backup-x.json')
    const pickCall = nativeCalls.find((call) => call.cmd === 'pick_save_path')
    expect(pickCall).toBeDefined()
    const options = pickCall?.args?.options as { default_file_name?: string }
    // Default name: floattranslate-backup-YYYYMMDD-HHmmss.json
    expect(options.default_file_name).toMatch(/^floattranslate-backup-\d{8}-\d{6}\.json$/)
    expect(body.find('[data-testid="data-notice"]').text()).toContain(
      'C:\\backups\\floattranslate-backup-x.json',
    )
    wrapper.unmount()
  })

  it('cancel in the picker does not call the backend', async () => {
    fakePicker(null)
    const { wrapper, body } = await mountSection()
    const api = await getClient()
    const exportSpy = vi.spyOn(api, 'exportBackup')

    await wrapper.find('[data-testid="backup-export"]').trigger('click')
    await flushPromises()

    expect(exportSpy).not.toHaveBeenCalled()
    expect(body.find('[data-testid="data-notice"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('DataSection backup import (Phase 5)', () => {
  it('shows the per-table counts summary on success', async () => {
    fakePicker('C:\\backups\\save.json')
    const { wrapper, body } = await mountSection()
    const api = await getClient()
    vi.spyOn(api, 'importBackup').mockResolvedValue({
      imported: {
        translation_history: 3,
        vocabulary: 2,
        terminology: 1,
        chats: 1,
        messages: 5,
        messages_skipped: 1,
      },
    })

    await wrapper.find('[data-testid="backup-import"]').trigger('click')
    await flushPromises()

    const notice = body.find('[data-testid="data-notice"]')
    expect(notice.exists()).toBe(true)
    expect(notice.text()).toContain('历史 3')
    expect(notice.text()).toContain('单词 2')
    expect(notice.text()).toContain('术语 1')
    expect(notice.text()).toContain('会话 1')
    expect(notice.text()).toContain('消息 5')
    wrapper.unmount()
  })

  it('surfaces BACKUP_VERSION_UNSUPPORTED as an upgrade hint', async () => {
    fakePicker('C:\\backups\\unsupported-version.json')
    const { wrapper, body } = await mountSection()
    const api = await getClient()
    const importSpy = vi.spyOn(api, 'importBackup')

    await wrapper.find('[data-testid="backup-import"]').trigger('click')
    await flushPromises()

    expect(importSpy).toHaveBeenCalledWith('C:\\backups\\unsupported-version.json')
    // The mock import sleeps before rejecting; wait for the inline error.
    await vi.waitFor(() => {
      expect(body.find('[data-testid="data-error"]').exists()).toBe(true)
    })
    const error = body.find('[data-testid="data-error"]')
    expect(error.text()).toContain('备份版本不受支持')
    expect(body.find('[data-testid="data-notice"]').exists()).toBe(false)
    wrapper.unmount()
  })
})

describe('DataSection reset app (double confirm, docs/05 §7)', () => {
  it('two confirms → resetApp + reset_window_state → onboarding with a cleared key', async () => {
    fakePicker(null)
    const { wrapper, router, body } = await mountSection()
    const settings = useSettingsStore()
    const api = await getClient()
    const resetSpy = vi.spyOn(api, 'resetApp')
    expect(settings.provider?.api_key_configured).toBe(true)

    await wrapper.find('[data-testid="reset-app"]').trigger('click')
    await flushPromises()
    // Step 1: plain confirm.
    expect(resetSpy).not.toHaveBeenCalled()
    await lastModalConfirm(body).trigger('click')
    await flushPromises()
    expect(resetSpy).not.toHaveBeenCalled()
    // Step 2: red danger confirm executes the reset.
    await lastModalConfirm(body).trigger('click')
    await flushPromises()

    expect(resetSpy).toHaveBeenCalledTimes(1)
    expect(nativeCalls.some((call) => call.cmd === 'reset_window_state')).toBe(true)
    await vi.waitFor(() => {
      expect(router.currentRoute.value.name).toBe('onboarding')
    })
    expect(settings.provider?.api_key_configured).toBe(false)
    wrapper.unmount()
  })

  it('cancel on step 1 never calls resetApp', async () => {
    fakePicker(null)
    const { wrapper, body } = await mountSection()
    const api = await getClient()
    const resetSpy = vi.spyOn(api, 'resetApp')

    await wrapper.find('[data-testid="reset-app"]').trigger('click')
    await flushPromises()
    const cancel = body.findAll('.modal-actions button')[0]
    await cancel.trigger('click')
    await flushPromises()

    expect(resetSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
