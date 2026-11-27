import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { provideGlobalConfig } from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'

/* Exact DESIGN.md v3.0 cascade order (EP JS tree-shaken via Vite; EP CSS imported once BEFORE overrides) */
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import './styles/fonts.css'
import './styles/tokens.css'
import './styles/element-overrides.css'
import './styles/base.css'
import './styles/utilities.css'

import App from './App.vue'
import router from './router'
import { messages } from './locales/messages'

const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('ki.lang') || 'zh-CN',
  fallbackLocale: 'zh-CN',
  messages,
})

const app = createApp(App)
provideGlobalConfig({ locale: zhCn }, app, true)
app.use(createPinia())
app.use(router)
app.use(i18n)
app.mount('#app')
