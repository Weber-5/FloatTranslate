<script setup lang="ts">
/**
 * Translation settings: terminology CRUD (case-insensitive unique source)
 * and the custom translation prompt. The prompt auto-saves on input with an
 * 800ms debounce (Phase 2 policy); terminology keeps explicit actions.
 */
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { toApiError } from '@/api'
import IconButton from '@/components/common/IconButton.vue'
import IconCheck from '@/components/icons/IconCheck.vue'
import IconClose from '@/components/icons/IconClose.vue'
import IconEdit from '@/components/icons/IconEdit.vue'
import IconPlus from '@/components/icons/IconPlus.vue'
import IconTrash from '@/components/icons/IconTrash.vue'
import LoadingState from '@/components/common/LoadingState.vue'

const { t } = useI18n()
const settings = useSettingsStore()

const addForm = reactive({ source: '', target: '' })
const addError = ref<string | null>(null)

const editingId = ref<string | null>(null)
const editForm = reactive({ source: '', target: '' })

const customPromptDraft = ref('')

onMounted(() => {
  void settings.loadTerminology()
})

// Sync the prompt draft on (re)load only; saves never clobber typing.
watch(
  () => settings.status,
  (status) => {
    if (status === 'success' && settings.app) {
      customPromptDraft.value = settings.app.custom_translation_prompt ?? ''
    }
  },
  { immediate: true },
)

const hasTerminology = computed(() => settings.terminology.length > 0)

async function addTerm(): Promise<void> {
  addError.value = null
  try {
    await settings.addTerm(addForm.source.trim(), addForm.target.trim())
    addForm.source = ''
    addForm.target = ''
  } catch (err) {
    addError.value = toApiError(err).message
  }
}

function startEdit(id: string, source: string, target: string): void {
  editingId.value = id
  editForm.source = source
  editForm.target = target
}

async function commitEdit(): Promise<void> {
  const id = editingId.value
  if (id && editForm.source.trim() && editForm.target.trim()) {
    await settings.updateTerm(id, editForm.source.trim(), editForm.target.trim())
  }
  editingId.value = null
}

function saveCustomPromptDebounced(): void {
  settings.saveAppDebounced({ custom_translation_prompt: customPromptDraft.value })
}
</script>

<template>
  <div class="settings-section card">
    <h2 class="section-title">{{ t('settings.section.translation') }}</h2>

    <div class="terminology">
      <p class="field-hint">{{ t('settings.translation.terminologyDesc') }}</p>

      <LoadingState v-if="settings.terminologyStatus === 'loading'" :label="t('common.loading')" />
      <p v-else-if="!hasTerminology" class="empty-terms">
        {{ t('settings.translation.terminologyEmpty') }}
      </p>
      <ul v-else class="term-list">
        <li v-for="term in settings.terminology" :key="term.id" class="term-row">
          <template v-if="editingId === term.id">
            <input v-model="editForm.source" class="input term-input" :aria-label="t('settings.translation.termSource')" />
            <span class="term-arrow" aria-hidden="true">→</span>
            <input v-model="editForm.target" class="input term-input" :aria-label="t('settings.translation.termTarget')" />
            <IconButton :label="t('common.save')" size="sm" @click="commitEdit">
              <IconCheck :size="14" />
            </IconButton>
            <IconButton :label="t('common.cancel')" size="sm" @click="editingId = null">
              <IconClose :size="14" />
            </IconButton>
          </template>
          <template v-else>
            <span class="term-source">{{ term.source }}</span>
            <span class="term-arrow" aria-hidden="true">→</span>
            <span class="term-target">{{ term.target }}</span>
            <IconButton
              :label="t('common.edit')"
              size="sm"
              @click="startEdit(term.id, term.source, term.target)"
            >
              <IconEdit :size="13" />
            </IconButton>
            <IconButton :label="t('common.delete')" size="sm" tone="danger" @click="settings.deleteTerm(term.id)">
              <IconTrash :size="13" />
            </IconButton>
          </template>
        </li>
      </ul>

      <form class="term-add" @submit.prevent="addTerm">
        <input
          v-model="addForm.source"
          class="input term-input"
          type="text"
          :placeholder="t('settings.translation.termSource')"
          :aria-label="t('settings.translation.termSource')"
        />
        <input
          v-model="addForm.target"
          class="input term-input"
          type="text"
          :placeholder="t('settings.translation.termTarget')"
          :aria-label="t('settings.translation.termTarget')"
        />
        <button type="submit" class="btn btn-secondary" :disabled="!addForm.source.trim() || !addForm.target.trim()">
          <IconPlus :size="14" />
          {{ t('settings.translation.addTerm') }}
        </button>
      </form>
      <p v-if="addError" class="term-error">{{ addError }}</p>
    </div>

    <div class="field">
      <span class="field-label">{{ t('settings.translation.customPrompt') }}</span>
      <textarea
        v-model="customPromptDraft"
        class="textarea"
        rows="3"
        :placeholder="t('settings.translation.customPromptPlaceholder')"
        :aria-label="t('settings.translation.customPrompt')"
        @input="saveCustomPromptDebounced"
      />
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

.terminology {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.field-hint,
.empty-terms {
  font-size: 11px;
  color: var(--text-tertiary);
}

.term-list {
  display: flex;
  flex-direction: column;
}

.term-row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 4px 0;
  border-bottom: 1px solid var(--hairline);
}

.term-row:last-child {
  border-bottom: none;
}

.term-source,
.term-target {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.term-source {
  flex: 1;
  min-width: 0;
}

.term-target {
  flex: 1;
  min-width: 0;
  color: var(--text-secondary);
}

.term-arrow {
  color: var(--text-tertiary);
  flex: none;
}

.term-input {
  padding: 5px 8px;
  font-size: 12px;
  flex: 1;
  min-width: 0;
}

.term-add {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding-top: var(--space-2);
}

.term-error {
  font-size: 12px;
  color: var(--danger);
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
</style>
