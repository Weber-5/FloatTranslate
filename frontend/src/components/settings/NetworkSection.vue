<script setup lang="ts">
/**
 * Network settings: proxy mode (system / HTTP / HTTPS / SOCKS5) with custom
 * fields (docs/00 §9). Mode (select) persists immediately; text fields
 * persist on change with an 800ms debounce (Phase 2 auto-save policy).
 */
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProxyMode } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'

const { t } = useI18n()
const settings = useSettingsStore()

const form = reactive({
  proxy_mode: 'system' as ProxyMode,
  proxy_host: '',
  proxy_port: 0,
  proxy_username: '',
  proxy_password: '',
})

function syncFromApp(): void {
  const app = settings.app
  if (!app) return
  form.proxy_mode = app.proxy_mode ?? 'system'
  form.proxy_host = app.proxy_host ?? ''
  form.proxy_port = app.proxy_port ?? 0
  form.proxy_username = app.proxy_username ?? ''
  form.proxy_password = app.proxy_password ?? ''
}

// Sync on (re)load only; background saves never clobber in-progress typing.
watch(
  () => settings.status,
  (status) => {
    if (status === 'success') syncFromApp()
  },
  { immediate: true },
)

const isCustom = computed(() => form.proxy_mode !== 'system')

function persistMode(): void {
  void settings.saveApp({
    proxy_mode: form.proxy_mode,
  })
}

function persistFieldsDebounced(): void {
  settings.saveAppDebounced({
    proxy_host: form.proxy_host.trim(),
    proxy_port: Number(form.proxy_port) || 0,
    proxy_username: form.proxy_username,
    proxy_password: form.proxy_password,
  })
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.network') }}</h2>

    <div class="proxy-modes" role="radiogroup" :aria-label="t('settings.network.proxy')">
      <label v-for="mode in ['system', 'http', 'https', 'socks5'] as const" :key="mode" class="proxy-option">
        <input
          v-model="form.proxy_mode"
          type="radio"
          name="proxy-mode"
          :value="mode"
          @change="persistMode"
        />
        <span>{{
          mode === 'system'
            ? t('settings.network.proxySystem')
            : mode === 'http'
              ? t('settings.network.proxyHttp')
              : mode === 'https'
                ? t('settings.network.proxyHttps')
                : t('settings.network.proxySocks5')
        }}</span>
      </label>
    </div>

    <div v-if="isCustom" class="proxy-fields">
      <label class="field">
        <span class="field-label">{{ t('settings.network.host') }}</span>
        <input v-model="form.proxy_host" type="text" class="input" @change="persistFieldsDebounced" />
      </label>
      <label class="field">
        <span class="field-label">{{ t('settings.network.port') }}</span>
        <input v-model.number="form.proxy_port" type="number" min="0" class="input" @change="persistFieldsDebounced" />
      </label>
      <label class="field">
        <span class="field-label">{{ t('settings.network.username') }}</span>
        <input v-model="form.proxy_username" type="text" class="input" autocomplete="off" @change="persistFieldsDebounced" />
      </label>
      <label class="field">
        <span class="field-label">{{ t('settings.network.password') }}</span>
        <input v-model="form.proxy_password" type="password" class="input" autocomplete="new-password" @change="persistFieldsDebounced" />
      </label>
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

.proxy-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-3);
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
</style>
