<script setup lang="ts">
/**
 * Network settings (docs/00 §9, Phase 5 contract): proxy mode radio
 * system / none / http / https / socks5 with a single proxy_url field shown
 * for the custom modes. Client-side validation requires the URL scheme to
 * match the mode (http:// / https:// / socks5://); invalid input shows an
 * inline error and is NOT saved. Mode persists immediately; the URL
 * persists on change with the 800ms debounce (Phase 2 auto-save policy).
 */
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProxyMode } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'
import { isValidProxyUrl, requiredProxyScheme } from '@/lib/proxy'

const { t } = useI18n()
const settings = useSettingsStore()

const PROXY_MODES = ['system', 'none', 'http', 'https', 'socks5'] as const

const form = reactive({
  proxy_mode: 'system' as ProxyMode,
  proxy_url: '',
})

// The inline error appears once the user commits the field (change/blur);
// pristine state never starts red.
const urlTouched = ref(false)

const urlInvalid = computed(() => {
  if (form.proxy_mode === 'system' || form.proxy_mode === 'none') return false
  return urlTouched.value && !isValidProxyUrl(form.proxy_mode, form.proxy_url)
})

const urlError = computed(() => {
  if (!urlInvalid.value) return null
  const scheme = requiredProxyScheme(form.proxy_mode)
  return t('settings.network.proxyUrlInvalid', { scheme: scheme?.replace(':', '') ?? '' })
})

function syncFromApp(): void {
  const app = settings.app
  if (!app) return
  form.proxy_mode = app.proxy_mode ?? 'system'
  form.proxy_url = app.proxy_url ?? ''
}

// Sync on (re)load only; background saves never clobber in-progress typing.
watch(
  () => settings.status,
  (status) => {
    if (status === 'success') syncFromApp()
  },
  { immediate: true },
)

const isCustom = computed(() => form.proxy_mode !== 'system' && form.proxy_mode !== 'none')

function persistMode(): void {
  // Switching modes re-validates the URL for the new scheme; an invalid URL
  // is never persisted for the new mode (inline error guides the fix).
  void settings.saveApp({ proxy_mode: form.proxy_mode })
}

function persistUrlDebounced(): void {
  urlTouched.value = true
  if (urlInvalid.value) return // inline error shown; field not saved
  settings.saveAppDebounced({ proxy_url: form.proxy_url.trim() })
}

function modeLabel(mode: ProxyMode): string {
  switch (mode) {
    case 'system':
      return t('settings.network.proxySystem')
    case 'none':
      return t('settings.network.proxyNone')
    case 'http':
      return t('settings.network.proxyHttp')
    case 'https':
      return t('settings.network.proxyHttps')
    default:
      return t('settings.network.proxySocks5')
  }
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.network') }}</h2>

    <div class="proxy-modes" role="radiogroup" :aria-label="t('settings.network.proxy')">
      <label v-for="mode in PROXY_MODES" :key="mode" class="proxy-option">
        <input
          v-model="form.proxy_mode"
          type="radio"
          name="proxy-mode"
          :value="mode"
          data-testid="proxy-mode"
          @change="persistMode"
        />
        <span>{{ modeLabel(mode) }}</span>
      </label>
    </div>

    <div v-if="isCustom" class="proxy-url" data-testid="proxy-url-wrap">
      <label class="field">
        <span class="field-label">{{ t('settings.network.proxyUrl') }}</span>
        <input
          v-model="form.proxy_url"
          type="text"
          class="input"
          :class="{ invalid: urlInvalid }"
          :placeholder="`${form.proxy_mode}://host:port`"
          autocomplete="off"
          spellcheck="false"
          data-testid="proxy-url"
          @change="persistUrlDebounced"
        />
      </label>
      <p v-if="urlInvalid" class="url-error" role="alert" data-testid="proxy-url-error">
        {{ urlError }}
      </p>
    </div>
  </div>
</template>

<style scoped>
.settings-section {
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.proxy-modes {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.proxy-option {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  padding: 5px 10px;
  border-radius: 999px;
  border: 1px solid var(--hairline-strong);
  cursor: pointer;
  color: var(--text-secondary);
  transition:
    color var(--transition-fast),
    border-color var(--transition-fast);
}

.proxy-option:has(input:checked) {
  color: var(--accent);
  border-color: var(--accent);
  background: var(--accent-soft);
}

.proxy-option input {
  accent-color: var(--accent);
  margin: 0;
}

.proxy-url {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.field-label {
  font-size: 12px;
  color: var(--text-secondary);
}

.input.invalid {
  border-color: var(--danger);
}

.url-error {
  font-size: 12px;
  color: var(--danger);
}
</style>
