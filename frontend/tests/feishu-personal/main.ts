import { createApp } from 'vue'
import { createI18n } from 'vue-i18n'
import { Button } from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import Harness from './Harness.vue'

const locale = new URLSearchParams(location.search).get('locale') || 'en-US'
createApp(Harness).component('t-button', Button).use(createI18n({ legacy: false, locale, messages: { [locale]: {} } })).mount('#app')
