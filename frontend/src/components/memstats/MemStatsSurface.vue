<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { useMemStatsStore } from '@/stores/memStats'
import MemStatsPanel from '@/components/memstats/MemStatsPanel.vue'

defineOptions({
  name: 'MemStatsSurface',
})

const store = useMemStatsStore()

onMounted(() => store.initialize())
onUnmounted(() => store.cleanup())
</script>

<template>
  <div class="flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-app-content">
    <MemStatsPanel class="w-full flex-1 overflow-hidden" />
  </div>
</template>
