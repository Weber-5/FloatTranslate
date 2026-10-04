<script setup lang="ts">
/**
 * General settings: theme, always-on-top, auto start, hotkey recorder
 * (docs/00 §9, docs/07 §4). Hotkey save sequence (frozen contract):
 * invoke apply_hotkeys FIRST — only when it reports registered do we PUT
 * the settings; on conflict we keep the previous value and show an inline
 * error, so the previous registrations remain active.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ThemeMode } from '@/api/types'
import { useSettingsStore } from '@/stores/settings'
import { applyHotkeys } from '@/services/hotkeys'
import { DEFAULT_HOTKEY_QUICK_TRANSLATE, DEFAULT_HOTKEY_TOGGLE } from '@/constants'
import SettingRow from './SettingRow.vue'
import HotkeyInput from './HotkeyInput.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const theme = computed<ThemeMode>(() => settings.app?.theme ?? 'system')
const alwaysOnTop = computed(() => settings.app?.always_on_top ?? false)
const autoStart = computed(() => settings.app?.auto_start ?? false)

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

function setAlwaysOnTop(checked: boolean): void {
  void settings.saveApp({ always_on_top: checked })
}

function setAutoStart(checked: boolean): void {
  void settings.saveApp({ auto_start: checked })
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
          type="checkbox"
          :checked="alwaysOnTop"
          :aria-label="t('settings.general.alwaysOnTop')"
          @change="setAlwaysOnTop(($event.target as HTMLInputElement).checked)"
        />
        <span class="switch-track" aria-hidden="true" />
      </label>
    </SettingRow>

    <SettingRow :label="t('settings.general.autoStart')" :hint="t('common.desktopOnly')">
      <label class="switch">
        <input
          type="checkbox"
          :checked="autoStart"
          :aria-label="t('settings.general.autoStart')"
          @change="setAutoStart(($event.target as HTMLInputElement).checked)"
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
</style>
