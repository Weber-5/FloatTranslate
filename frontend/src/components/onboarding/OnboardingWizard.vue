<script setup lang="ts">
/**
 * First-launch wizard (docs/00 §10): provider mode / base URL / API key /
 * models / test connection. The backend tests the SUBMITTED config; only a
 * successful test persists via PUT /settings/provider and enters the main
 * window. There is no skip — a key must be configured first (router guard).
 */
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import type { ProviderMode, ProviderSettingsUpdate } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'
import IconButton from '@/components/common/IconButton.vue'
import IconEye from '@/components/icons/IconEye.vue'
import IconEyeOff from '@/components/icons/IconEyeOff.vue'

const { t } = useI18n()
const router = useRouter()
const settings = useSettingsStore()

const DEEPSEEK_PRESET_BASE_URL = 'https://api.deepseek.com'
const DEFAULT_MODEL = 'deepseek-flash'

const form = reactive({
  mode: 'deepseek' as ProviderMode,
  api_key: '',
  base_url: DEEPSEEK_PRESET_BASE_URL,
  translation_model: DEFAULT_MODEL,
  chat_model: DEFAULT_MODEL,
})

const baseUrlTouched = ref(false)
const showKey = ref(false)
const testing = ref(false)
const error = ref<string | null>(null)

watch(
  () => form.mode,
  (mode) => {
    // Apply the deepseek preset unless the user typed a custom base URL.
    if (mode === 'deepseek' && !baseUrlTouched.value) {
      form.base_url = DEEPSEEK_PRESET_BASE_URL
    }
  },
)

function buildUpdate(): ProviderSettingsUpdate {
  return {
    mode: form.mode,
    base_url: form.base_url.trim(),
    translation_model: form.translation_model.trim() || DEFAULT_MODEL,
    chat_model: form.chat_model.trim() || DEFAULT_MODEL,
    api_key: form.api_key.trim(),
  }
}

/**
 * Test with the CURRENT form values; on success persist the same values and
 * re-fetch capabilities before entering the main UI. On failure: stay here.
 */
async function testAndSave(): Promise<void> {
  error.value = null
  const key = form.api_key.trim()
  if (key.length === 0) {
    error.value = t('onboarding.needTest')
    return
  }
  const update = buildUpdate()
  testing.value = true
  try {
    const result = await settings.testConnection(update)
    if (!result.ok) {
      error.value = `${t('settings.provider.testFailed')}：${result.message ?? ''}`
      return
    }
    await settings.saveProvider(update)
    // Re-fetch provider + capabilities with the now-working configuration.
    await settings.load(true)
    form.api_key = ''
    showKey.value = false
    await router.push('/translate')
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('errors.unknown')
  } finally {
    testing.value = false
  }
}

const busyLabel = computed(() => (testing.value ? t('onboarding.testing') : t('onboarding.saveAndTest')))
</script>

<template>
  <div class="onboarding">
    <!-- Frameless window: this top strip is the only drag handle on the
         onboarding screen — without it the window cannot be moved at all. -->
    <div class="onboarding-drag-strip" data-tauri-drag-region aria-hidden="true" />
    <div class="wizard card">
      <h1 class="wizard-title">{{ t('onboarding.title') }}</h1>
      <p class="wizard-desc">{{ t('onboarding.desc') }}</p>

      <label class="field">
        <span class="field-label">{{ t('settings.provider.mode') }}</span>
        <select v-model="form.mode" class="select" data-testid="onboarding-mode">
          <option value="deepseek">{{ t('settings.provider.modeDeepseek') }}</option>
          <option value="openai_compatible">{{ t('settings.provider.modeOpenai') }}</option>
        </select>
      </label>

      <label class="field">
        <span class="field-label">{{ t('onboarding.apiKey') }}</span>
        <span class="key-row">
          <input
            v-model="form.api_key"
            :type="showKey ? 'text' : 'password'"
            class="input"
            :placeholder="t('onboarding.apiKeyPlaceholder')"
            autocomplete="off"
            data-testid="onboarding-api-key"
            :aria-label="t('onboarding.apiKey')"
          />
          <IconButton
            :label="t('common.open')"
            size="sm"
            class="key-toggle"
            @click="showKey = !showKey"
          >
            <IconEyeOff v-if="showKey" :size="15" />
            <IconEye v-else :size="15" />
          </IconButton>
        </span>
      </label>

      <label class="field">
        <span class="field-label">{{ t('onboarding.baseUrl') }}</span>
        <input
          v-model="form.base_url"
          type="text"
          class="input"
          data-testid="onboarding-base-url"
          @input="baseUrlTouched = true"
        />
      </label>

      <label class="field">
        <span class="field-label">{{ t('onboarding.translationModel') }}</span>
        <input v-model="form.translation_model" type="text" class="input" />
      </label>

      <label class="field">
        <span class="field-label">{{ t('onboarding.chatModel') }}</span>
        <input v-model="form.chat_model" type="text" class="input" />
      </label>

      <p v-if="error" class="notice notice-error" role="alert" data-testid="onboarding-error">
        {{ error }}
      </p>

      <div class="wizard-actions">
        <button
          type="button"
          class="btn btn-primary wizard-test"
          :disabled="testing"
          data-testid="onboarding-test"
          @click="testAndSave"
        >
          {{ busyLabel }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.onboarding {
  position: relative;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: var(--space-5);
  background: var(--bg-app);
}

.onboarding-drag-strip {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 28px;
  /* Same stacking context as the centered card; keeps the strip grabbable. */
  z-index: 10;
}

.wizard {
  width: min(360px, 100%);
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.wizard-title {
  font-size: 20px;
  font-weight: 700;
}

.wizard-desc {
  font-size: 13px;
  color: var(--text-secondary);
  line-height: 1.6;
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

.wizard-actions {
  display: flex;
  justify-content: stretch;
  gap: var(--space-2);
  margin-top: var(--space-2);
}

.wizard-test {
  flex: 1;
}
</style>
