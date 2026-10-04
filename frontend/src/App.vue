<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppShell from '@/components/layout/AppShell.vue'
import BackendStatusGate from '@/components/common/BackendStatusGate.vue'
import { useTheme } from '@/composables/useTheme'
import { useBackendStore } from '@/stores/backend'

const route = useRoute()
useTheme()
const backend = useBackendStore()

// Real mode: nothing renders until the local backend answered /health.
// Mock mode (pure browser dev) is unaffected.
const gateUp = computed(() => backend.isRealMode && !backend.ready)
const showBare = computed(() => route.meta.bare === true && !gateUp.value)
const showShell = computed(() => !gateUp.value && route.meta.bare !== true)
</script>

<template>
  <!-- Onboarding runs as a bare full-window wizard; everything else lives in the shell. -->
  <router-view v-if="showBare" />
  <AppShell v-else-if="showShell" />
  <BackendStatusGate v-if="gateUp" />
</template>
