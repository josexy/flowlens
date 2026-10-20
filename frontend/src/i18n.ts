import { createI18n } from 'vue-i18n'
import en from './locales/en.json'
import zh from './locales/zh.json'
import pythonPluginHelpEn from './locales/python-plugin-help/en.json'
import pythonPluginHelpZh from './locales/python-plugin-help/zh.json'

const i18n = createI18n({
  legacy: false, // Set to false to use Composition API
  locale: 'zh', // Set default locale
  fallbackLocale: 'en', // Set fallback locale
  messages: {
    en: { ...en, python_plugin_help: pythonPluginHelpEn },
    zh: { ...zh, python_plugin_help: pythonPluginHelpZh },
  },
})

export default i18n
