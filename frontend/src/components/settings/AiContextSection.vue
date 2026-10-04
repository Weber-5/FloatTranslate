<script setup lang="ts">
/**
 * AI Context settings (docs/06 §9): context/output token limits, auto
 * compact + threshold, Global Context, editable System Prompt with restore
 * default, and the "configured vs effective" clamp notice.
 * Auto-save policy (Phase 2): text/number fields persist on change with an
 * 800ms debounce; toggles persist immediately.
 */
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { DEFAULT_AI_SYSTEM_PROMPT } from '@/constants'
import SettingRow from './SettingRow.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const form = reactive({
  context_tokens: 1000000,
  output_tokens: 4096,
  auto_compact: true,
  compact_threshold: 80,
  ai_system_prompt: '',
})

const globalContextDraft = ref('')
const systemPromptSaved = ref(false)

function syncFromApp(): void {
  const app = settings.app
  if (!app) return
  form.context_tokens = app.context_tokens ?? 1000000
  form.output_tokens = app.output_tokens ?? 4096
  form.auto_compact = app.auto_compact ?? true
  form.compact_threshold = app.compact_threshold ?? 80
  form.ai_system_prompt = app.ai_system_prompt ?? ''
}

// Sync on (re)load only — never on background saves, so an 800ms debounced
// save cannot clobber a field the user is still typing into.
watch(
  () => settings.status,
  (status) => {
    if (status === 'success') syncFromApp()
  },
  { immediate: true },
)

watch(
  () => settings.globalContext,
  (value) => {
    if (globalContextDraft.value !== value) globalContextDraft.value = value
  },
  { immediate: true },
)

const clampNoticeText = computed(() => {
  const notice = settings.clampNotice
  if (!notice) return null
  return t('settings.aiContext.clampNotice', {
    configured: notice.configured.toLocaleString('en-US'),
    effective: notice.effective.toLocaleString('en-US'),
    configuredOut: notice.configuredOut.toLocaleString('en-US'),
    effectiveOut: notice.effectiveOut.toLocaleString('en-US'),
  })
})

function persistNumericDebounced(): void {
  settings.saveAppDebounced({
    context_tokens: Number(form.context_tokens) || 0,
    output_tokens: Number(form.output_tokens) || 0,
    auto_compact: form.auto_compact,
    compact_threshold: Number(form.compact_threshold),
  })
}

function persistAutoCompact(): void {
  void settings.saveApp({ auto_compact: form.auto_compact })
}

function persistSystemPromptDebounced(): void {
  settings.saveAppDebounced({ ai_system_prompt: form.ai_system_prompt })
}

async function restoreDefaultPrompt(): Promise<void> {
  form.ai_system_prompt = DEFAULT_AI_SYSTEM_PROMPT
  await settings.saveApp({ ai_system_prompt: DEFAULT_AI_SYSTEM_PROMPT })
  systemPromptSaved.value = true
  window.setTimeout(() => {
    systemPromptSaved.value = false
  }, 2500)
}

function saveGlobalContextDebounced(): void {
  settings.saveGlobalContextDebounced(globalContextDraft.value)
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.aiContext') }}</h2>

    <p
      v-if="clampNoticeText"
      class="clamp-notice"
      role="status"
    >
      {{ clampNoticeText }}
    </p>

    <SettingRow :label="t('settings.aiContext.contextTokens')">
      <input
        v-model.number="form.context_tokens"
        type="number"
        min="0"
        class="input input-number"
        :aria-label="t('settings.aiContext.contextTokens')"
        @change="persistNumericDebounced"
      />
    </SettingRow>

    <SettingRow :label="t('settings.aiContext.outputTokens')">
      <input
        v-model.number="form.output_tokens"
        type="number"
        min="0"
        class="input input-number"
        :aria-label="t('settings.aiContext.outputTokens')"
        @change="persistNumericDebounced"
      />
    </SettingRow>

    <SettingRow :label="t('settings.aiContext.autoCompact')">
      <label class="switch">
        <input
          v-model="form.auto_compact"
          type="checkbox"
          :aria-label="t('settings.aiContext.autoCompact')"
          @change="persistAutoCompact"
        />
        <span class="switch-track" aria-hidden="true" />
      </label>
    </SettingRow>

    <SettingRow :label="`${t('settings.aiContext.threshold')}：${form.compact_threshold}%`">
      <input
        v-model.number="form.compact_threshold"
        type="range"
        min="50"
        max="95"
        step="5"
        class="threshold-slider"
        :aria-label="t('settings.aiContext.threshold')"
        @change="persistNumericDebounced"
      />
    </SettingRow>

    <div class="field">
      <span class="field-label">{{ t('settings.aiContext.globalContext') }}</span>
      <textarea
        v-model="globalContextDraft"
        class="textarea"
        rows="3"
        :placeholder="t('settings.aiContext.globalContextPlaceholder')"
        :aria-label="t('settings.aiContext.globalContext')"
        @input="saveGlobalContextDebounced"
      />
      <p v-if="settings.globalContextSaving" class="autosave-note" role="status">
        {{ t('common.saving') }}
      </p>
    </div>

    <div class="field">
      <span class="field-label">{{ t('settings.aiContext.systemPrompt') }}</span>
      <textarea
        v-model="form.ai_system_prompt"
        class="textarea"
        rows="4"
        :aria-label="t('settings.aiContext.systemPrompt')"
        @input="persistSystemPromptDebounced"
      />
      <div class="field-actions">
        <span v-if="systemPromptSaved" class="saved-note">{{ t('common.saved') }}</span>
        <button type="button" class="btn btn-ghost" @click="restoreDefaultPrompt">
          {{ t('settings.aiContext.restoreDefault') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-section {
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.clamp-notice {
  font-size: 12px;
  line-height: 1.6;
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-md);
  background: var(--warning-soft);
  color: var(--warning);
}

.input-number {
  width: 140px;
  text-align: right;
}

.threshold-slider {
  width: 140px;
  accent-color: var(--accent);
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--space-2) 0;
}

.field-label {
  font-size: 12px;
  color: var(--text-secondary);
  font-weight: 500;
}

.field-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
}

.saved-note {
  font-size: 11px;
  color: var(--success);
}

.autosave-note {
  font-size: 11px;
  color: var(--text-tertiary);
  text-align: right;
}

.switch {
  position: relative;
  display: inline-flex;
  align-items: center;
  cursor: pointer;
}

.switch input {
  position: absolute;
  opacity: 0;
  width: 100%;
  height: 100%;
  margin: 0;
  cursor: pointer;
}

.switch-track {
  width: 38px;
  height: 22px;
  border-radius: 999px;
  background: var(--hairline-strong);
  transition: background var(--transition-fast);
  position: relative;
}

.switch-track::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: #fff;
  box-shadow: var(--shadow-1);
  transition: transform var(--transition-fast);
}

.switch input:checked + .switch-track {
  background: var(--success);
}

.switch input:checked + .switch-track::after {
  transform: translateX(16px);
}

.switch input:focus-visible + .switch-track {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
</style>
