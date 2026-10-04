<script setup lang="ts">
/**
 * First-launch wizard (docs/00 §10): API key / base URL / models /
 * test connection, then enter the main window. Only reachable when
 * `api_key_configured` is false (router guard).
 */
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useSettingsStore } from '@/stores/settings'
import IconButton from '@/components/common/IconButton.vue'
import IconEye from '@/components/icons/IconEye.vue'
import IconEyeOff from '@/components/icons/IconEyeOff.vue'

const { t } = useI18n()
const router = useRouter()
const settings = useSettingsStore()

const form = reactive({
  api_key: '',
  base_url: 'https://api.deepseek.com',
  translation_model: 'deepseek-flash',
  chat_model: 'deepseek-flash',
})

const showKey = ref(false)
const testing = ref(false)
const testOk = ref(false)
const testMessage = ref<string | null>(null)
const error = ref<string | null>(null)

const canEnter = computed(() => testOk.value)

async function saveAndTest(): Promise<void> {
  error.value = null
  testMessage.value = null
  const key = form.api_key.trim()
  if (key.length === 0) {
    error.value = t('onboarding.needTest')
    return
  }
  testing.value = true
  try {
    await settings.saveProvider({
      mode: 'deepseek',
      base_url: form.base_url.trim(),
      translation_model: form.translation_model.trim(),
      chat_model: form.chat_model.trim(),
      api_key: key,
    })
    form.api_key = ''
    showKey.value = false
    const result = await settings.testConnection()
    testOk.value = result.ok
    testMessage.value = result.message ?? null
    if (!result.ok) {
      error.value = `${t('settings.provider.testFailed')}：${result.message ?? ''}`
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('errors.unknown')
  } finally {
    testing.value = false
  }
}

function enter(): void {
  void router.push('/translate')
}
</script>

<template>
  <div class="onboarding">
    <div class="wizard card">
      <h1 class="wizard-title">{{ t('onboarding.title') }}</h1>
      <p class="wizard-desc">{{ t('onboarding.desc') }}</p>

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
        <input v-model="form.base_url" type="text" class="input" data-testid="onboarding-base-url" />
      </label>

      <label class="field">
        <span class="field-label">{{ t('onboarding.translationModel') }}</span>
        <input v-model="form.translation_model" type="text" class="input" />
      </label>

      <label class="field">
        <span class="field-label">{{ t('onboarding.chatModel') }}</span>
        <input v-model="form.chat_model" type="text" class="input" />
      </label>

      <p v-if="error" class="notice notice-error" role="alert">{{ error }}</p>
      <p v-else-if="testOk" class="notice notice-success" role="status">
        {{ t('settings.provider.testOk') }}{{ testMessage ? `：${testMessage}` : '' }}
      </p>

      <div class="wizard-actions">
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="testing"
          data-testid="onboarding-test"
          @click="saveAndTest"
        >
          {{ testing ? t('onboarding.testing') : t('onboarding.saveAndTest') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!canEnter"
          data-testid="onboarding-enter"
          @click="enter"
        >
          {{ t('onboarding.enter') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.onboarding {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-5);
  background: var(--bg-app);
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
  justify-content: space-between;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
</style>
