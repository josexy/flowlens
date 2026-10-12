import '@wailsio/runtime'
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ui from '@nuxt/ui/vue-plugin'

import i18n from './i18n'
import './style.css'

async function bootstrap() {
  const startup = window.location.hash === '#/reset-startup'
  let app
  if (startup) {
    const [{ default: Root }, { createMemoryHistory, createRouter }] = await Promise.all([
      import('./views/ResetStartupView.vue'),
      import('vue-router'),
    ])
    app = createApp(Root)
    app.use(createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: Root }] }))
    document.documentElement.classList.toggle('dark', window.matchMedia('(prefers-color-scheme: dark)').matches)
  } else {
    const [{ default: Root }, { default: router }] = await Promise.all([
      import('./App.vue'),
      import('./router'),
    ])
    app = createApp(Root)
    app.use(createPinia())
    app.use(router)
    await router.isReady()
  }
  app.use(i18n)
  app.use(ui)
  app.mount('#app')
}

void bootstrap().catch((error) => console.error('Start frontend failed', error))
