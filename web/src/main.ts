import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'

import './styles/tokens.css'
import './styles/element-overrides.css'
import './styles/base.css'

import App from './App.vue'
import router from './router'
import { messages } from './locales/messages'

const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('kinstall.lang') || 'zh-CN',
  fallbackLocale: 'zh-CN',
  messages,
})

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)
app.use(ElementPlus)
app.mount('#app')
