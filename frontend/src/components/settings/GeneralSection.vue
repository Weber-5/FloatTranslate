<script setup lang="ts">
/**
 * General settings: theme, always-on-top, auto start, hotkey recorder
 * (docs/00 §9, docs/07 §4). Hotkey save sequence (frozen contract):
 * invoke apply_hotkeys FIRST — only when it reports registered do we PUT
 * the settings; on conflict we keep the previous value and show an inline
 * error, so the previous registrations remain active.
 *
 * Phase 5 native toggles: always-on-top / auto start invoke the host command
 * FIRST, then persist the setting. When the host command is missing the UI
 * shows the unsupported notice and keeps the previous value. The auto start
 * toggle syncs its boot state from get_autostart (real host) and is disabled
 * with a tooltip when that state cannot be read.
 */
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ThemeMode } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'
import { applyHotkeys } from '@/services/hotkeys'
import { getAutostart, setAutostart, setAlwaysOnTop } from '@/services/native'
import { DEFAULT_HOTKEY_QUICK_TRANSLATE, DEFAULT_HOTKEY_TOGGLE } from '@/constants'
import SettingRow from './SettingRow.vue'
import HotkeyInput from './HotkeyInput.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const theme = computed<ThemeMode>(() => settings.app?.theme ?? 'system')
const autoStartSupported = ref(true)

/** Template refs so a failed command can force the checkbox back (Vue skips
 * re-patching a prop that returned to its original value). */
const alwaysOnTopInput = ref<HTMLInputElement | null>(null)
const autoStartInput = ref<HTMLInputElement | null>(null)

/**
 * Optimistic local overrides so the switch reflects the native state even
 * while the settings PUT is in flight; null → fall back to the saved setting.
 */
const alwaysOnTopOverride = ref<boolean | null>(null)
const autoStartOverride = ref<boolean | null>(null)

const alwaysOnTop = computed(() => alwaysOnTopOverride.value ?? settings.app?.always_on_top ?? false)
const autoStart = computed(() => autoStartOverride.value ?? settings.app?.auto_start ?? false)

/** Reverts a switch after a failed native command (state + DOM checkbox). */
async function revertSwitch(
  override: typeof alwaysOnTopOverride,
  input: typeof alwaysOnTopInput,
  previous: boolean,
): Promise<void> {
  override.value = previous
  await nextTick()
  if (input.value) input.value.checked = previous
}

onMounted(async () => {
  // Sync the auto start toggle from the OS registration (real host); when
  // the host cannot answer, disable the toggle with a tooltip instead.
  const current = await getAutostart()
  if (current.unsupported || !current.ok) {
    autoStartSupported.value = false
    return
  }
  autoStartOverride.value = current.value ?? false
})

function showNotice(message: string): void {
  window.setTimeout(() => {
    if (notice.value === message) notice.value = null
  }, 4000)
  notice.value = message
}

const notice = ref<string | null>(null)

async function onAlwaysOnTopChange(checked: boolean): Promise<void> {
  const previous = alwaysOnTop.value
  alwaysOnTopOverride.value = checked
  const result = await setAlwaysOnTop(checked)
  if (!result.ok) {
    await revertSwitch(alwaysOnTopOverride, alwaysOnTopInput, previous)
    showNotice(t('common.unsupported'))
    return
  }
  void settings.saveApp({ always_on_top: checked })
  alwaysOnTopOverride.value = null
}

async function onAutoStartChange(checked: boolean): Promise<void> {
  if (!autoStartSupported.value) return
  const previous = autoStart.value
  autoStartOverride.value = checked
  const result = await setAutostart(checked)
  if (!result.ok) {
    await revertSwitch(autoStartOverride, autoStartInput, previous)
    showNotice(t('common.unsupported'))
    return
  }
  void settings.saveApp({ auto_start: checked })
  autoStartOverride.value = null
}

const hotkeyToggle = computed(
  () => settings.app?.hotkey_toggle_window ?? DEFAULT_HOTKEY_TOGGLE,
)
const hotkeyQuick = computed(
  () => settings.app?.hotkey_quick_translate ?? DEFAULT_HOTKEY_QUICK_TRANSLATE,
)

type HotkeyField = 'hotkey_toggle_window' | 'hotkey_quick_translate'

const hotkeySaving = ref<HotkeyField | null>(null)
const hotkeyConflict = ref<string | null>(null)

async function onHotkeyCommit(field: HotkeyField, value: string): Promise<void> {
  const app = settings.app
  if (!app || hotkeySaving.value !== null) return
  if (value === (app[field] ?? (field === 'hotkey_toggle_window' ? DEFAULT_HOTKEY_TOGGLE : DEFAULT_HOTKEY_QUICK_TRANSLATE))) {
    return
  }
  const pair = {
    show_hide: field === 'hotkey_toggle_window' ? value : hotkeyToggle.value,
    translate_selection: field === 'hotkey_quick_translate' ? value : hotkeyQuick.value,
  }
  hotkeySaving.value = field
  try {
    const result = await applyHotkeys(pair)
    if (result.registered) {
      hotkeyConflict.value = null
      await settings.saveApp({ [field]: value })
    } else {
      // Do not PUT: the setting stays at its previous value, which the
      // recorder displays again automatically once recording ends.
      hotkeyConflict.value = result.conflict ?? value
    }
  } catch {
    hotkeyConflict.value = value
  } finally {
    hotkeySaving.value = null
  }
}

const themeOptions: { value: ThemeMode; label: string }[] = [
  { value: 'system', label: 'settings.general.themeSystem' },
  { value: 'light', label: 'settings.general.themeLight' },
  { value: 'dark', label: 'settings.general.themeDark' },
]

function setTheme(mode: ThemeMode): void {
  void settings.saveApp({ theme: mode })
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.general') }}</h2>

    <SettingRow :label="t('settings.general.theme')">
      <div class="segmented" role="radiogroup" :aria-label="t('settings.general.theme')">
        <button
          v-for="option in themeOptions"
          :key="option.value"
          type="button"
          class="segment"
          role="radio"
          :aria-checked="theme === option.value"
          :class="{ active: theme === option.value }"
          @click="setTheme(option.value)"
        >
          {{ t(option.label) }}
        </button>
      </div>
    </SettingRow>

    <SettingRow :label="t('settings.general.alwaysOnTop')" :hint="t('common.desktopOnly')">
      <label class="switch">
        <input
          ref="alwaysOnTopInput"
          type="checkbox"
          :checked="alwaysOnTop"
          :aria-label="t('settings.general.alwaysOnTop')"
          data-testid="always-on-top"
          @change="onAlwaysOnTopChange(($event.target as HTMLInputElement).checked)"
        />
        <span class="switch-track" aria-hidden="true" />
      </label>
      <p v-if="notice" class="row-notice" role="status" data-testid="general-notice">{{ notice }}</p>
    </SettingRow>

    <SettingRow
      :label="t('settings.general.autoStart')"
      :hint="autoStartSupported ? t('common.desktopOnly') : t('settings.general.autoStartUnsupported')"
    >
      <label class="switch" :title="autoStartSupported ? undefined : t('common.unsupported')">
        <input
          ref="autoStartInput"
          type="checkbox"
          :checked="autoStart"
          :disabled="!autoStartSupported"
          :aria-label="t('settings.general.autoStart')"
          data-testid="auto-start"
          @change="onAutoStartChange(($event.target as HTMLInputElement).checked)"
        />
        <span class="switch-track" aria-hidden="true" />
      </label>
    </SettingRow>

    <SettingRow
      :label="t('settings.general.hotkeyToggle')"
      :hint="t('settings.general.hotkeyRecordHint')"
    >
      <HotkeyInput
        :model-value="hotkeyToggle"
        :label="t('settings.general.hotkeyToggle')"
        data-testid="hotkey-toggle"
        @update:model-value="(value: string) => onHotkeyCommit('hotkey_toggle_window', value)"
      />
    </SettingRow>

    <SettingRow
      :label="t('settings.general.hotkeyQuick')"
      :hint="t('settings.general.hotkeyRecordHint')"
    >
      <HotkeyInput
        :model-value="hotkeyQuick"
        :label="t('settings.general.hotkeyQuick')"
        data-testid="hotkey-quick"
        @update:model-value="(value: string) => onHotkeyCommit('hotkey_quick_translate', value)"
      />
    </SettingRow>

    <p
      v-if="hotkeyConflict"
      class="hotkey-conflict"
      role="alert"
      data-testid="hotkey-conflict"
    >
      {{ t('settings.general.hotkeyConflict', { conflict: hotkeyConflict }) }}
    </p>
  </div>
</template>

<style scoped>
.settings-section {
  padding: var(--space-4) var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.segmented {
  display: flex;
  gap: 2px;
  background: var(--bg-inset);
  border-radius: var(--radius-md);
  padding: 2px;
}

.segment {
  padding: 5px 12px;
  border-radius: var(--radius-sm);
  font-size: 12px;
  color: var(--text-secondary);
  transition:
    background var(--transition-fast),
    color var(--transition-fast);
}

.segment.active {
  background: var(--bg-surface);
  color: var(--text-primary);
  box-shadow: var(--shadow-1);
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

.hotkey-conflict {
  font-size: 12px;
  color: var(--danger);
}

.row-notice {
  margin-top: 4px;
  font-size: 12px;
  color: var(--text-tertiary);
}
</style>
