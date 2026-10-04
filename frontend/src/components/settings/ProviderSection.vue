<script setup lang="ts">
/**
 * Provider settings (docs/00 §9): mode / base URL / API key / models /
 * test connection. The API key input is write-only: it starts empty, is
 * never prefetched as plaintext, and is cleared after save.
 */
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ProviderMode } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'
import IconButton from '@/components/common/IconButton.vue'
import IconEye from '@/components/icons/IconEye.vue'
import IconEyeOff from '@/components/icons/IconEyeOff.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const form = reactive({
  mode: 'deepseek' as ProviderMode,
  base_url: '',
  translation_model: '',
  chat_model: '',
})

const apiKeyInput = ref('')
const showKey = ref(false)
const savedNotice = ref(false)

watch(
  () => settings.provider,
  (provider) => {
    if (!provider) return
    form.mode = provider.mode
    form.base_url = provider.base_url
    form.translation_model = provider.translation_model
    form.chat_model = provider.chat_model
  },
  { immediate: true, deep: true },
)

const configured = computed(() => settings.provider?.api_key_configured ?? false)

// The password input always starts empty (write-only key). The placeholder
// reflects the configured state without ever revealing stored material.
const keyPlaceholder = computed(() =>
  configured.value
    ? t('settings.provider.apiKeyConfiguredPlaceholder')
    : t('settings.provider.apiKeyEmptyPlaceholder'),
)

const keyHint = computed(() => {
  const hint = settings.provider?.api_key_hint ?? ''
  const masked = hint.length > 0 ? `（${hint}）` : ''
  return `${t('settings.provider.apiKeySaveHint')}${masked}`
})

async function save(): Promise<void> {
  const key = apiKeyInput.value.trim()
  await settings.saveProvider({
    mode: form.mode,
    base_url: form.base_url.trim(),
    translation_model: form.translation_model.trim(),
    chat_model: form.chat_model.trim(),
    ...(key.length > 0 ? { api_key: key } : {}),
  })
  // Plaintext is dropped immediately after save; it can never be read back.
  apiKeyInput.value = ''
  showKey.value = false
  savedNotice.value = true
  window.setTimeout(() => {
    savedNotice.value = false
  }, 2500)
}

/** Test the CURRENT form values (backend tests the submitted config). */
async function testConnection(): Promise<void> {
  const key = apiKeyInput.value.trim()
  await settings.testConnection({
    mode: form.mode,
    base_url: form.base_url.trim(),
    translation_model: form.translation_model.trim(),
    chat_model: form.chat_model.trim(),
    ...(key.length > 0 ? { api_key: key } : {}),
  })
}
</script>

<template>
  <div id="settings-provider" class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.provider') }}</h2>

    <div class="form-grid">
      <label class="field">
        <span class="field-label">{{ t('settings.provider.mode') }}</span>
        <select v-model="form.mode" class="select" data-testid="provider-mode">
          <option value="deepseek">{{ t('settings.provider.modeDeepseek') }}</option>
          <option value="openai_compatible">{{ t('settings.provider.modeOpenai') }}</option>
        </select>
      </label>

      <label class="field">
        <span class="field-label">{{ t('settings.provider.baseUrl') }}</span>
        <input v-model="form.base_url" type="text" class="input" data-testid="provider-base-url" />
      </label>

      <label class="field">
        <span class="field-label">{{ t('settings.provider.apiKey') }}</span>
        <span class="key-row">
          <input
            v-model="apiKeyInput"
            :type="showKey ? 'text' : 'password'"
            class="input"
            :placeholder="keyPlaceholder"
            autocomplete="off"
            data-testid="provider-api-key-input"
            :aria-label="t('settings.provider.apiKey')"
          />
          <IconButton
            :label="showKey ? t('common.close') : t('common.open')"
            size="sm"
            class="key-toggle"
            @click="showKey = !showKey"
          >
            <IconEyeOff v-if="showKey" :size="15" />
            <IconEye v-else :size="15" />
          </IconButton>
        </span>
        <span class="field-hint">
          {{ keyHint }}
        </span>
      </label>

      <label class="field">
        <span class="field-label">{{ t('settings.provider.translationModel') }}</span>
        <input
          v-model="form.translation_model"
          type="text"
          class="input"
          data-testid="provider-translation-model"
        />
      </label>

      <label class="field">
        <span class="field-label">{{ t('settings.provider.chatModel') }}</span>
        <input v-model="form.chat_model" type="text" class="input" data-testid="provider-chat-model" />
      </label>
    </div>

    <div class="actions">
      <button type="button" class="btn btn-secondary" :disabled="settings.savingProvider" data-testid="provider-save" @click="save">
        {{ t('settings.provider.save') }}
      </button>
      <button
        type="button"
        class="btn btn-ghost"
        :disabled="settings.providerTestRunning"
        data-testid="provider-test"
        @click="testConnection"
      >
        {{ settings.providerTestRunning ? t('settings.provider.testing') : t('settings.provider.test') }}
      </button>
    </div>

    <p v-if="savedNotice" class="notice notice-success" data-testid="provider-saved">
      {{ t('common.saved') }}
    </p>
    <p
      v-if="settings.providerTestResult"
      class="notice"
      :class="settings.providerTestResult.ok ? 'notice-success' : 'notice-error'"
      data-testid="provider-test-result"
    >
      {{
        settings.providerTestResult.ok
          ? `${t('settings.provider.testOk')}：${settings.providerTestResult.message ?? ''}`
          : `${t('settings.provider.testFailed')}：${settings.providerTestResult.message ?? ''}`
      }}
    </p>
  </div>
</template>

<style scoped>
.settings-section {
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.form-grid {
  display: flex;
  flex-direction: column;
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
  font-weight: 500;
}

.field-hint {
  font-size: 11px;
  color: var(--text-tertiary);
  line-height: 1.5;
}

.key-row {
  position: relative;
  display: flex;
  align-items: center;
}

.key-row .input {
  padding-right: 34px;
}

.key-toggle {
  position: absolute;
  right: 4px;
}

.actions {
  display: flex;
  gap: var(--space-2);
}

.notice {
  font-size: 12px;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
}

.notice-success {
  background: var(--success-soft);
  color: var(--success);
}

.notice-error {
  background: var(--danger-soft);
  color: var(--danger);
}
</style>
