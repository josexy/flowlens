<script setup lang="ts">
import { computed, ref } from 'vue'
import { parseHighlightedUrl } from '@/utils/urlHighlight'

const modelValue = defineModel<string>({ required: true })

const props = withDefaults(
  defineProps<{
    placeholder?: string
  }>(),
  {
    placeholder: '',
  },
)

const inputRef = ref<HTMLInputElement | null>(null)
const scrollLeft = ref(0)

const isEmpty = computed(() => !modelValue.value)

const parsedUrl = computed(() => {
  return parseHighlightedUrl(modelValue.value)
})

function handleInput(event: Event) {
  modelValue.value = (event.target as HTMLInputElement).value
}

function handleScroll(event: Event) {
  scrollLeft.value = (event.target as HTMLInputElement).scrollLeft
}

function focusInput() {
  inputRef.value?.focus()
}

defineExpose({
  focus: () => inputRef.value?.focus(),
})
</script>

<template>
  <div
    class="relative h-(--request-url-height,28px) w-full min-w-0 cursor-text overflow-hidden rounded-[inherit] [border-width:var(--request-url-border-width,1px)] border-(--request-url-border-color,var(--app-border-color)) bg-(--request-url-bg,var(--app-panel-bg))"
    :class="$slots.suffix ? '[--request-url-pr:var(--request-url-suffix-padding,44px)]' : '[--request-url-pr:var(--request-url-padding-x,5px)]'"
    @click="focusInput"
  >
    <div
      class="pointer-events-none absolute inset-0 flex items-center overflow-hidden whitespace-pre py-0 pl-(--request-url-padding-x,5px) pr-(--request-url-pr) text-(length:--request-url-font-size,13px) font-normal leading-(--request-url-line-height,20px)"
      aria-hidden="true"
    >
      <div
        class="inline-block min-w-max leading-[inherit]"
        :style="{ transform: `translateX(-${scrollLeft}px)` }"
      >
        <template v-if="!isEmpty">
          <span class="text-app-text-muted">{{ parsedUrl.scheme }}</span>
          <span class="text-[#16a34a]">{{ parsedUrl.host }}</span>
          <span class="text-[#21a2df]">{{ parsedUrl.path }}</span>
          <template v-if="parsedUrl.hasQuery">
            <span class="text-[rgb(202,36,36)]">?</span>
            <template v-for="(item, index) in parsedUrl.queryItems" :key="`query-${index}`">
              <span v-if="index > 0" class="text-[rgb(202,36,36)]">&</span>
              <span class="text-[#7c3aed]">{{ item.key }}</span>
              <span v-if="item.hasEquals" class="text-[rgb(202,36,36)]">=</span>
              <span v-if="item.hasEquals" class="text-[#f59e0b]">{{ item.value }}</span>
            </template>
          </template>
          <span class="text-[#ec4899]">{{ parsedUrl.hash }}</span>
        </template>
      </div>
    </div>
    <input
      ref="inputRef"
      class="absolute inset-x-0 top-1/2 h-(--request-url-line-height,20px) w-full -translate-y-1/2 whitespace-pre border-none bg-transparent py-0 pl-(--request-url-padding-x,5px) pr-(--request-url-pr) text-(length:--request-url-font-size,13px) font-normal leading-(--request-url-line-height,20px) text-transparent caret-app-text outline-none placeholder:text-app-text-muted"
      type="text"
      :placeholder="props.placeholder"
      spellcheck="false"
      :value="modelValue"
      @input="handleInput"
      @scroll="handleScroll"
    />
    <div
      v-if="$slots.suffix"
      class="absolute right-(--request-url-suffix-right,5px) top-1/2 z-2 flex -translate-y-1/2 items-center justify-center"
    >
      <slot name="suffix" />
    </div>
  </div>
</template>
