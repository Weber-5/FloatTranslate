<script setup lang="ts">
/**
 * Sanitized Markdown renderer for chat messages (docs/00 §7: Markdown、代码
 * 块、复制). `marked` parses, DOMPurify sanitizes with a strict whitelist —
 * raw HTML, scripts and event handlers never reach the DOM. Code blocks get
 * a language label and a copy button, attached by enhancing the rendered
 * `pre` elements after each update (event delegation for the copy clicks).
 */
import { computed, nextTick, ref, watch } from 'vue'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { useI18n } from 'vue-i18n'
import { copyText } from '@/services/clipboard'

const props = defineProps<{ source: string }>()

const { t } = useI18n()

// Strict whitelist: no raw HTML beyond markdown's structural tags, no
// images (no remote content in 1.0), no styles/scripts/iframes/forms.
const ALLOWED_TAGS = [
  'p',
  'br',
  'hr',
  'strong',
  'b',
  'em',
  'i',
  'del',
  's',
  'code',
  'pre',
  'blockquote',
  'ul',
  'ol',
  'li',
  'h1',
  'h2',
  'h3',
  'h4',
  'h5',
  'h6',
  'a',
  'span',
  'table',
  'thead',
  'tbody',
  'tr',
  'th',
  'td',
]
const ALLOWED_ATTR = ['class', 'href', 'title', 'target', 'rel', 'start']

marked.use({
  gfm: true,
  breaks: true,
})

const html = computed(() => {
  const raw = marked.parse(props.source, { async: false }) as string
  return DOMPurify.sanitize(raw, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    FORBID_ATTR: ['style', 'srcset'],
  })
})

const container = ref<HTMLElement | null>(null)

function labelFor(pre: HTMLElement): string {
  const code = pre.querySelector('code')
  const classes = code?.className ?? ''
  const match = /language-([\w+-]+)/.exec(classes)
  return match ? match[1] : t('ai.codePlain')
}

/** Wraps each `pre` in a card with a language label + copy button. */
function enhanceCodeBlocks(): void {
  const el = container.value
  if (!el) return
  for (const pre of Array.from(el.querySelectorAll('pre'))) {
    if (pre.parentElement?.classList.contains('md-code')) continue
    const wrapper = document.createElement('div')
    wrapper.className = 'md-code'
    const header = document.createElement('div')
    header.className = 'md-code-head'
    const label = document.createElement('span')
    label.className = 'md-code-lang'
    label.textContent = labelFor(pre)
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'md-code-copy'
    button.setAttribute('data-md-copy', '')
    button.textContent = t('ai.copyCode')
    header.appendChild(label)
    header.appendChild(button)
    pre.replaceWith(wrapper)
    wrapper.appendChild(header)
    wrapper.appendChild(pre)
  }
}

async function onCopyClick(event: MouseEvent): Promise<void> {
  const target = event.target as HTMLElement | null
  const button = target?.closest('[data-md-copy]')
  if (!(button instanceof HTMLElement)) return
  const code = button.closest('.md-code')?.querySelector('pre code')
  const ok = await copyText(code?.textContent ?? '')
  if (ok) {
    button.textContent = t('ai.copied')
    window.setTimeout(() => {
      button.textContent = t('ai.copyCode')
    }, 1600)
  }
}

watch(
  html,
  () => {
    void nextTick(enhanceCodeBlocks)
  },
  { immediate: true },
)
</script>

<template>
  <!-- eslint-disable vue/no-v-html — the only v-html in this component renders
       DOMPurify-sanitized markdown with a strict tag/attr whitelist. -->
  <div
    ref="container"
    class="markdown"
    data-testid="markdown-content"
    @click="onCopyClick"
    v-html="html"
  />
  <!-- eslint-enable vue/no-v-html -->
</template>

<style scoped>
.markdown {
  font-size: 13px;
  line-height: 1.65;
  word-break: break-word;
  overflow-wrap: anywhere;
}

.markdown :deep(p) {
  margin: 0 0 var(--space-2);
  white-space: pre-wrap;
}

.markdown :deep(p:last-child) {
  margin-bottom: 0;
}

.markdown :deep(ul),
.markdown :deep(ol) {
  margin: 0 0 var(--space-2);
  padding-left: 1.4em;
}

.markdown :deep(blockquote) {
  margin: 0 0 var(--space-2);
  padding: 2px var(--space-3);
  border-left: 2px solid var(--hairline-strong);
  color: var(--text-secondary);
}

.markdown :deep(h1),
.markdown :deep(h2),
.markdown :deep(h3),
.markdown :deep(h4),
.markdown :deep(h5),
.markdown :deep(h6) {
  font-size: 13.5px;
  font-weight: 600;
  margin: var(--space-2) 0 var(--space-1);
}

.markdown :deep(a) {
  color: var(--accent);
}

.markdown :deep(code) {
  font-family: var(--font-mono);
  font-size: 12px;
  background: var(--bg-inset);
  border-radius: 4px;
  padding: 1px 4px;
}

.markdown :deep(.md-code) {
  margin: 0 0 var(--space-2);
  border: 1px solid var(--hairline);
  border-radius: var(--radius-md);
  overflow: hidden;
  background: var(--bg-inset);
}

.markdown :deep(.md-code-head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 3px 8px;
  border-bottom: 1px solid var(--hairline);
  background: var(--bg-surface-2);
}

.markdown :deep(.md-code-lang) {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-tertiary);
}

.markdown :deep(.md-code-copy) {
  font-size: 11px;
  color: var(--text-secondary);
  padding: 2px 8px;
  border-radius: var(--radius-sm);
}

.markdown :deep(.md-code-copy:hover) {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.markdown :deep(.md-code pre) {
  margin: 0;
  padding: var(--space-2) var(--space-3);
  overflow-x: auto;
}

.markdown :deep(.md-code pre code) {
  background: transparent;
  padding: 0;
}

.markdown :deep(hr) {
  border: none;
  border-top: 1px solid var(--hairline);
  margin: var(--space-2) 0;
}

.markdown :deep(table) {
  border-collapse: collapse;
  margin: 0 0 var(--space-2);
  font-size: 12.5px;
}

.markdown :deep(th),
.markdown :deep(td) {
  border: 1px solid var(--hairline);
  padding: 3px 8px;
}
</style>
