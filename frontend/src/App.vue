<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppShell from '@/components/layout/AppShell.vue'
import BackendStatusGate from '@/components/common/BackendStatusGate.vue'
import { useTheme } from '@/composables/useTheme'
import { useBackendStore } from '@/stores/backend'
import { registerHotkeysFromSettings } from '@/services/hotkeys'
import { listenSelectionCaptured } from '@/services/selection'

const route = useRoute()
const router = useRouter()
useTheme()
const backend = useBackendStore()

// Real mode: nothing renders until the local backend answered /health.
// Mock mode (pure browser dev) is unaffected.
const gateUp = computed(() => backend.isRealMode && !backend.ready)
const showBare = computed(() => route.meta.bare === true && !gateUp.value)
const showShell = computed(() => !gateUp.value && route.meta.bare !== true)

// US-04: the host emits `selection-captured` after waking the window; the
// handler opens a new tab and auto-sends the translation.
onMounted(() => {
  void listenSelectionCaptured()
})

// Frozen boot sequence: once health is ready (probe or host event), register
// the stored hotkeys from the loaded settings — exactly once per launch.
const hotkeysApplied = ref(false)
watch(
  () => backend.isRealMode && backend.ready,
  (shouldApply) => {
    if (!shouldApply) return
    // improvement bug #4: the initial navigation was aborted by the backend
    // gate, leaving the body blank until the user clicked 翻译. When no route
    // is actually mounted, (re)enter the default one; the router guard
    // redirects to onboarding when no API key is configured.
    if (route.matched.length === 0) {
      void router.replace({ name: 'translate' })
    }
    if (hotkeysApplied.value) return
    hotkeysApplied.value = true
    void registerHotkeysFromSettings().catch(() => undefined)
  },
  { immediate: true },
)
</script>

<template>
  <!-- Onboarding runs as a bare full-window wizard; everything else lives in the shell. -->
  <router-view v-if="showBare" />
  <AppShell v-else-if="showShell" />
  <BackendStatusGate v-if="gateUp" />
</template>
