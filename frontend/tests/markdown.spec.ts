/**
 * Markdown rendering (docs/00 §7): sanitize (no scripts/raw HTML), code
 * blocks with language label + copy button.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import MarkdownContent from '@/components/common/MarkdownContent.vue'
import { i18n } from '@/i18n'

function mountMarkdown(source: string) {
  return mount(MarkdownContent, {
    props: { source },
    global: { plugins: [i18n] },
  })
}

beforeEach(() => {
  Object.defineProperty(window.navigator, 'clipboard', {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    configurable: true,
  })
})

describe('MarkdownContent', () => {
  it('strips script tags and raw HTML from the rendered output', async () => {
    const wrapper = mountMarkdown(
      'hello <script>alert(1)</script><img src=x onerror="alert(1)"> **bold**',
    )
    await wrapper.vm.$nextTick()
    const html = wrapper.find('[data-testid="markdown-content"]').element.innerHTML
    expect(html).not.toContain('<script')
    expect(html).not.toContain('onerror')
    expect(html).not.toContain('<img')
    expect(html).toContain('hello')
    expect(html).toContain('<strong>bold</strong>')
  })

  it('renders fenced code blocks with a language label and a copy button', async () => {
    const wrapper = mountMarkdown('```ts\nconst a = 1\n```\n')
    // The code-block enhancement runs after a nextTick.
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.md-code').exists()).toBe(true)
    expect(wrapper.find('.md-code-lang').text()).toBe('ts')
    expect(wrapper.find('[data-md-copy]').exists()).toBe(true)
    expect(wrapper.find('.md-code pre code').text()).toContain('const a = 1')
  })

  it('labels code blocks without a language with the plain label', async () => {
    const wrapper = mountMarkdown('```\nplain\n```\n')
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.md-code-lang').text()).toBe(i18n.global.t('ai.codePlain'))
  })

  it('copies the code text when the copy button is clicked', async () => {
    const wrapper = mountMarkdown('```js\nconsole.log(42)\n```\n')
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-md-copy]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('console.log(42)\n')
    expect(wrapper.find('[data-md-copy]').text()).toBe(i18n.global.t('ai.copied'))
  })

  it('renders streaming updates without losing the copy affordance', async () => {
    const wrapper = mountMarkdown('partial')
    await wrapper.setProps({ source: '```py\nx = 1\n```' })
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.md-code-lang').text()).toBe('py')
  })
})
