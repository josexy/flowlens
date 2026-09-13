<script setup lang="ts">
type ProxyToolbarIconName =
  | 'mode'
  | 'listen-host'
  | 'port'
  | 'upstream-proxy'
  | 'http2'
  | 'anti-cache'
  | 'anti-comp'
  | 'tls-verify'
  | 'host-filter'

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
      <circle cx="4.75" cy="7" r="1.5" />
      <circle cx="4.75" cy="17" r="1.5" />
      <circle cx="19.25" cy="7" r="1.5" />
      <circle cx="19.25" cy="17" r="1.5" />
      <path d="M6.25 7h2.25c2.1 0 3.5 1.4 3.5 3.5v3c0 2.1 1.4 3.5 3.5 3.5h2.25M6.25 17h2.25c2.1 0 3.5-1.4 3.5-3.5v-3C12 8.4 13.4 7 15.5 7h2.25" />
      <path d="m16.75 5.5 2 1.5-2 1.5M16.75 15.5l2 1.5-2 1.5" />
    </template>

    <template v-else-if="name === 'listen-host'">
      <rect x="4.5" y="7" width="10.5" height="10" rx="2" />
      <circle cx="7.5" cy="12" r="1.25" />
      <path d="M10.5 9.5h2.5M10.5 12h2.5M10.5 14.5h2.5M17 9.25c1.5 1.5 1.5 4 0 5.5M19 7.25c2.5 2.5 2.5 7 0 9.5" />
    </template>

    <template v-else-if="name === 'port'">
      <rect x="5" y="4" width="14" height="16" rx="2.5" />
      <path d="M9 7.5v9M15 7.5v9M7.5 11h9M7.5 14.5h9" />
    </template>

    <template v-else-if="name === 'upstream-proxy'">
      <circle cx="4.25" cy="12" r="1.5" />
      <rect x="9" y="8" width="6" height="8" rx="2" />
      <circle cx="19.75" cy="12" r="1.5" />
      <path d="M5.75 12H9M15 12h3.25M7.25 10.5 5.75 12l1.5 1.5M16.75 10.5l1.5 1.5-1.5 1.5" />
      <path v-if="active" d="M5 4 19 20" />
    </template>

    <template v-else-if="name === 'http2'">
      <path d="M4.5 7v10M10 7v10M4.5 12H10" />
      <path
        d="M14.5 9c.12-.95.8-1.6 1.85-1.6 1.15 0 1.9.65 1.9 1.6 0 1.55-3.8 1.95-3.8 3.9h3.95"
      />
      <path v-if="active" d="M5 4 19 20" />
    </template>

    <template v-else-if="name === 'anti-cache'">
      <ellipse cx="12" cy="6.5" rx="6.5" ry="2.75" />
      <path d="M5.5 6.5v5.5c0 1.5 2.9 2.75 6.5 2.75s6.5-1.25 6.5-2.75V6.5M5.5 12v5.5c0 1.5 2.9 2.75 6.5 2.75s6.5-1.25 6.5-2.75V12" />
      <path v-if="active" d="M4.5 4.5 19.5 19.5" />
    </template>

    <template v-else-if="name === 'anti-comp'">
      <rect x="5" y="5" width="14" height="14" rx="2" />
      <path d="M9 9h6M9 12h6M9 15h3" />
      <path v-if="active" d="M4.5 4.5 19.5 19.5" />
    </template>

    <template v-else-if="name === 'tls-verify'">
      <path d="M12 3.5 19 6.2v5.3c0 4.25-2.9 7.1-7 8.9-4.1-1.8-7-4.65-7-8.9V6.2Z" />
      <path v-if="!active" d="m8.5 11.7 2.5 2.5 4.7-4.7" />
      <path v-else d="M5 4 19 20" />
    </template>

    <template v-else>
      <path d="M4 6h16l-6 6v5l-4 2v-7Z" />
      <path d="M7.5 8.5h9M8.5 10.75h7" />
    </template>
  </svg>
</template>
