import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { setupFreshEnv } from './helpers'
import { useSettingsStore } from '@/stores/settings'
import { APP_VERSION } from '@/constants'
import {
  checkForUpdate,
  compareSemver,
  parseSemver,
  fetchLatestRelease,
  UpdateCheckError,
} from '@/services/update'
import { __setNativeInvokeForTests } from '@/services/native'
import AboutSection from '@/components/settings/AboutSection.vue'

function releaseResponse(tag: string): Response {
  return new Response(
    JSON.stringify({ tag_name: tag, html_url: `https://github.com/Weber-5/FloatTranslate/releases/tag/${tag}` }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  )
}

/** A fetch that never answers until its AbortSignal fires (hanging GitHub). */
function hangingFetch(): typeof fetch {
  return ((_url: string, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => {
        const error = new Error('aborted')
        error.name = 'AbortError'
        reject(error)
      })
    })) as unknown as typeof fetch
}

/**
 * The next patch release after the running version. Derived from APP_VERSION so
 * these tests never need editing when the app version is bumped (they used to
 * hardcode "v1.0.2 is newer", which broke on the 1.0.2 release).
 */
function nextPatch(version: string): string {
  const [major, minor, patch] = version.split('.').map(Number)
  return `v${major}.${minor}.${(patch || 0) + 1}`
}

/** A deliberately older release tag. */
const OLDER_TAG = 'v0.9.0'

afterEach(() => {
  __setNativeInvokeForTests(null)
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('semver comparison (docs/10 §5)', () => {
  it('parses v-prefixed tags and ignores pre-release suffixes', () => {
    expect(parseSemver('v1.2.3')).toEqual({ major: 1, minor: 2, patch: 3 })
    expect(parseSemver('1.2.3')).toEqual({ major: 1, minor: 2, patch: 3 })
    expect(parseSemver('v1.2.3-rc.1')).toEqual({ major: 1, minor: 2, patch: 3 })
    expect(parseSemver('nonsense')).toBeNull()
  })

  it('compares major/minor/patch lexicographically', () => {
    expect(compareSemver(nextPatch(APP_VERSION), APP_VERSION)).toBeGreaterThan(0)
    expect(compareSemver(`v${APP_VERSION}`, APP_VERSION)).toBe(0)
    expect(compareSemver('v0.9.9', APP_VERSION)).toBeLessThan(0)
    expect(compareSemver('v2.0.0', 'v1.99.99')).toBeGreaterThan(0)
    expect(compareSemver('v1.10.0', 'v1.9.9')).toBeGreaterThan(0)
    // Unparseable tags never claim an update.
    expect(compareSemver('nonsense', APP_VERSION)).toBe(0)
  })
})

describe('update service', () => {
  it('fetches the frozen GitHub releases endpoint with the Accept header', async () => {
    let url = ''
    let accept: string | undefined
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        url = String(input)
        accept = (init?.headers as Record<string, string>).Accept
        return releaseResponse('v1.0.1')
      }),
    )
    const latest = await fetchLatestRelease()
    expect(url).toBe('https://api.github.com/repos/Weber-5/FloatTranslate/releases/latest')
    expect(accept).toBe('application/vnd.github+json')
    expect(latest).not.toBeNull()
    expect(latest!.tag_name).toBe('v1.0.1')
  })

  it('rejects with UpdateCheckError on network/HTTP failures', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new TypeError('offline')
      }),
    )
    await expect(fetchLatestRelease()).rejects.toBeInstanceOf(UpdateCheckError)
    vi.stubGlobal('fetch', vi.fn(async () => new Response('nope', { status: 500 })))
    await expect(fetchLatestRelease()).rejects.toBeInstanceOf(UpdateCheckError)
  })

  it('checkForUpdate classifies newer vs equal releases', async () => {
    const newer = await checkForUpdate(APP_VERSION, { tag_name: nextPatch(APP_VERSION) })
    expect(newer.status).toBe('update-available')
    const same = await checkForUpdate(APP_VERSION, { tag_name: `v${APP_VERSION}` })
    expect(same.status).toBe('up-to-date')
    const older = await checkForUpdate(APP_VERSION, { tag_name: OLDER_TAG })
    expect(older.status).toBe('up-to-date')
  })

  it('reports "no published release" (HTTP 404) as its own state', async () => {
    // improvement bug #7: a repo without a public release answers 404. That is
    // neither an error nor a real "you are up to date" comparison.
    vi.stubGlobal('fetch', vi.fn(async () => new Response('Not Found', { status: 404 })))
    const latest = await fetchLatestRelease()
    expect(latest).toBeNull()
    const result = await checkForUpdate(APP_VERSION)
    expect(result.status).toBe('no-release')
    expect(result.latest).toBeNull()
  })
})

describe('AboutSection update check UI', () => {
  async function mountAbout(): Promise<{ wrapper: VueWrapper }> {
    const { i18n } = await setupFreshEnv()
    const settings = useSettingsStore()
    await settings.load()
    const wrapper = mount(AboutSection, { global: { plugins: [i18n] } })
    await flushPromises()
    return { wrapper }
  }

  it('shows the app version and the mock badge', async () => {
    const { wrapper } = await mountAbout()
    expect(wrapper.find('[data-testid="app-version"]').text()).toBe(`v${APP_VERSION}`)
    expect(wrapper.text()).toContain('Mock')
    wrapper.unmount()
  })

  it('newer release → banner with version, release link and URL fallback', async () => {
    const newerTag = nextPatch(APP_VERSION)
    vi.stubGlobal('fetch', vi.fn(async () => releaseResponse(newerTag)))
    const { wrapper } = await mountAbout()

    await wrapper.find('[data-testid="check-update"]').trigger('click')
    await flushPromises()

    const banner = wrapper.find('[data-testid="update-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain(newerTag)
    const link = wrapper.find('[data-testid="update-link"]')
    expect(link.attributes('href')).toBe(
      `https://github.com/Weber-5/FloatTranslate/releases/tag/${newerTag}`,
    )
    expect(link.attributes('rel')).toContain('noopener')
    expect(banner.text()).toContain(`releases/tag/${newerTag}`)
    wrapper.unmount()
  })

  it('equal version → up-to-date toast, no banner', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => releaseResponse(`v${APP_VERSION}`)))
    const { wrapper } = await mountAbout()

    await wrapper.find('[data-testid="check-update"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="update-banner"]').exists()).toBe(false)
    expect(wrapper.find('.notice').text()).toContain('已是最新版本')
    wrapper.unmount()
  })

  it('no published release → explicit notice, never a false "up to date"', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('Not Found', { status: 404 })))
    const { wrapper } = await mountAbout()

    await wrapper.find('[data-testid="check-update"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="update-banner"]').exists()).toBe(false)
    expect(wrapper.find('.notice').text()).toContain('尚未检测到已发布的版本')
    expect(wrapper.find('[data-testid="update-error"]').exists()).toBe(false)
    wrapper.unmount()
  })

  // UX review 2026-10-07: the result used to auto-hide after 4 s, so a user who
  // looked away saw nothing at all and assumed the button was dead.
  it('update result is sticky and carries the check time', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('Not Found', { status: 404 })))
    const { wrapper } = await mountAbout()

    await wrapper.find('[data-testid="check-update"]').trigger('click')
    await flushPromises()

    const notice = wrapper.find('[data-testid="about-notice"]')
    expect(notice.exists()).toBe(true)
    expect(notice.text()).toContain('检查于')
    // Still there well after the old 4 s auto-hide window.
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(wrapper.find('[data-testid="about-notice"]').exists()).toBe(true)
    wrapper.unmount()
  })

  // A hanging GitHub must not leave the user with a disabled button forever.
  it('a hanging request times out with its own message and a retry', async () => {
    vi.useFakeTimers()
    try {
      vi.stubGlobal('fetch', hangingFetch())
      const { wrapper } = await mountAbout()

      await wrapper.find('[data-testid="check-update"]').trigger('click')
      await vi.advanceTimersByTimeAsync(10_000)
      await flushPromises()

      const error = wrapper.find('[data-testid="update-error"]')
      expect(error.exists()).toBe(true)
      expect(error.text()).toContain('超时')
      expect(wrapper.find('[data-testid="update-retry"]').exists()).toBe(true)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('opens external links through the host (target=_blank is denied in the webview)', async () => {
    // improvement bug #7: wry marks new-window requests handled when Tauri
    // installs no new-window handler, so the anchor alone did nothing.
    const invoked: Array<{ cmd: string; args?: Record<string, unknown> }> = []
    __setNativeInvokeForTests(async (cmd, args) => {
      invoked.push({ cmd, args })
      return undefined
    })
    vi.stubGlobal('fetch', vi.fn(async () => releaseResponse(nextPatch(APP_VERSION))))
    const { wrapper } = await mountAbout()

    await wrapper.find('[data-testid="check-update"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="update-link"]').trigger('click')
    await flushPromises()
    await wrapper.find('[data-testid="github-link"]').trigger('click')
    await flushPromises()

    expect(invoked).toEqual([
      {
        cmd: 'open_external_url',
        args: { url: `https://github.com/Weber-5/FloatTranslate/releases/tag/${nextPatch(APP_VERSION)}` },
      },
      {
        cmd: 'open_external_url',
        args: { url: 'https://github.com/Weber-5/FloatTranslate' },
      },
    ])
    wrapper.unmount()
  })

  it('network failure → retryable inline error that recovers', async () => {
    const fetchMock = vi.fn(async (): Promise<Response> => {
      throw new TypeError('offline')
    })
    vi.stubGlobal('fetch', fetchMock)
    const { wrapper } = await mountAbout()

    await wrapper.find('[data-testid="check-update"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="update-error"]').exists()).toBe(true)

    // Retry with a working network → banner appears. The tag must be NEWER
    // than the running version, so derive it (a hardcoded tag broke the moment
    // the app reached that version).
    fetchMock.mockImplementation(async () => releaseResponse(nextPatch(APP_VERSION)))
    await wrapper.find('[data-testid="update-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="update-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="update-banner"]').text()).toContain(nextPatch(APP_VERSION))
    wrapper.unmount()
  })

  it('open logs dir: mock mode shows the success notice; no clear-logs button', async () => {
    const { wrapper } = await mountAbout()
    await wrapper.find('[data-testid="open-logs"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('.notice').text()).toContain('日志目录已打开')
    expect(wrapper.text()).not.toContain('清空日志')
    wrapper.unmount()
  })
})
