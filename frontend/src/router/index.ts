import { createRouter, createWebHashHistory } from 'vue-router'

const MainView = () => import('@/views/MainView.vue')
const SettingsView = () => import('@/views/SettingsView.vue')
const UpdateView = () => import('@/views/UpdateView.vue')

const router = createRouter({
  history: createWebHashHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'main',
      component: MainView,
    },
    {
      path: '/settings',
      name: 'settings',
      component: SettingsView,
    },
    {
      path: '/update',
      name: 'update',
      component: UpdateView,
    },
  ],
})

export default router
