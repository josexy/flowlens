<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  PROCESS_ICON_KEY_PATTERN,
  deleteProcessIconCacheEntry,
  loadProcessIcon,
  onProcessIconAvailable,
  onProcessIconCacheReset,
} from './processIconLoader'

const props = withDefaults(
  defineProps<{
    iconKey?: string | null
    alt?: string
    size?: number
  }>(),
  {
    iconKey: '',
    alt: '',
    size: 16,
  },
)

const source = ref<string | null>(null)
const iconSize = computed(() => `${Math.max(12, Math.min(64, props.size))}px`)

let mounted = false
let requestToken = 0
let offIconAvailable: (() => void) | null = null
let offCacheReset: (() => void) | null = null

async function loadCurrentIcon() {
  const token = ++requestToken
  const key = props.iconKey?.trim() ?? ''
  source.value = null

  if (!mounted || !key) {
    return
  }

  const nextSource = await loadProcessIcon(key)
  if (mounted && token === requestToken) {
    source.value = nextSource
  }
}

function handleImageError() {
  const key = props.iconKey?.trim() ?? ''
  if (PROCESS_ICON_KEY_PATTERN.test(key)) {
    deleteProcessIconCacheEntry(key)
  }
  source.value = null
}

watch(
  () => props.iconKey,
  () => {
    if (mounted) {
      void loadCurrentIcon()
    }
  },
)

onMounted(() => {
  mounted = true
  offIconAvailable = onProcessIconAvailable((key, availableSource) => {
    if (key === props.iconKey?.trim()) {
      source.value = availableSource
    }
  })
  offCacheReset = onProcessIconCacheReset(loadCurrentIcon)
  void loadCurrentIcon()
})

onBeforeUnmount(() => {
  mounted = false
  requestToken++
  offIconAvailable?.()
  offIconAvailable = null
  offCacheReset?.()
  offCacheReset = null
})
</script>

<template>
  <span
    class="inline-flex shrink-0 items-center justify-center overflow-hidden"
    :style="{ width: iconSize, height: iconSize }"
    :data-process-icon-key="iconKey || undefined"
  >
    <img
      v-if="source"
      :src="source"
      :alt="alt"
      class="size-full object-contain"
      draggable="false"
      @error="handleImageError"
    />
    <UIcon
      v-else
      name="i-lucide-app-window"
      class="size-full text-muted"
      aria-hidden="true"
    />
  </span>
</template>
