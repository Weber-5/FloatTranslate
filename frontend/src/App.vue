<script setup lang="ts">
import { useRoute } from 'vue-router'
import AppShell from '@/components/layout/AppShell.vue'
import { useTheme } from '@/composables/useTheme'

const route = useRoute()
useTheme()
</script>

<template>
  <!-- Onboarding runs as a bare full-window wizard; everything else lives in the shell. -->
  <router-view v-if="route.meta.bare === true" />
  <AppShell v-else />
</template>
