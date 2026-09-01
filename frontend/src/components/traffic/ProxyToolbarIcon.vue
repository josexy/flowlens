<script setup lang="ts">
type ProxyToolbarIconName =
  'mode' | 'listen-host' | 'port' | 'upstream-proxy' | 'http2' | 'tls-verify' | 'host-filter'

withDefaults(
  defineProps<{
    name: ProxyToolbarIconName
    active?: boolean
  }>(),
  {
    active: false,
  },
)
</script>

<template>
  <svg
    class="pointer-events-none block size-5 shrink-0 overflow-visible [shape-rendering:geometricPrecision]"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.75"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <template v-if="name === 'mode'">
      <circle cx="4.5" cy="7.25" r="1.5" />
      <circle cx="4.5" cy="16.75" r="1.5" />
      <circle cx="19.5" cy="7.25" r="1.5" />
      <circle cx="19.5" cy="16.75" r="1.5" />
      <path d="M6 7.25h2c2.75 0 3.25 9.5 6 9.5h4M6 16.75h2c2.75 0 3.25-9.5 6-9.5h4" />
    </template>

    <template v-else-if="name === 'listen-host'">
      <rect x="4" y="7" width="11" height="10" rx="2" />
      <circle cx="7.5" cy="12" r="1.25" />
      <path d="M10.5 10h2M10.5 14h2M17 9.25c1.5 1.5 1.5 4 0 5.5M19 7.25c2.5 2.5 2.5 7 0 9.5" />
    </template>

    <template v-else-if="name === 'port'">
      <rect x="5" y="4" width="14" height="16" rx="2.5" />
      <path d="M9.75 8.5l-1 7M14.75 8.5l-1 7M7.75 11h8.5M7.25 14h8.5" />
    </template>

    <template v-else-if="name === 'upstream-proxy'">
      <circle cx="4.25" cy="12" r="1.5" />
      <rect x="9" y="8" width="6" height="8" rx="2" />
      <circle cx="19.75" cy="12" r="1.5" />
      <path d="M5.75 12H9M15 12h3.25M16.75 10.5l1.5 1.5-1.5 1.5" />
      <path v-if="active" d="M5 4 19 20" />
    </template>

    <template v-else-if="name === 'http2'">
      <path d="M4.5 7v10M10 7v10M4.5 12H10" />
      <path
        d="M14.25 7.75c.15-1.15 1-1.9 2.35-1.9 1.4 0 2.25.8 2.25 1.9 0 1.9-4.6 2.35-4.6 4.75h4.6"
      />
      <path v-if="active" d="M5 4 19 20" />
    </template>

    <template v-else-if="name === 'tls-verify'">
      <path d="M12 3.5 19 6.2v5.3c0 4.25-2.9 7.1-7 8.9-4.1-1.8-7-4.65-7-8.9V6.2Z" />
      <path v-if="!active" d="m8.5 11.7 2.5 2.5 4.7-4.7" />
      <path v-else d="M5 4 19 20" />
    </template>

    <template v-else>
      <circle cx="6.5" cy="5.25" r="1" />
      <circle cx="12" cy="5.25" r="1" />
      <circle cx="17.5" cy="5.25" r="1" />
      <path d="M4.5 8h15L14 14.25v4L10 20v-5.75Z" />
    </template>
  </svg>
</template>
