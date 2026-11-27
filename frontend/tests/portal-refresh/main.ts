import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import TDesign from 'tdesign-vue-next'
import i18n from '../../src/i18n'
import PortalHome from '../../src/views/portal/PortalHome.vue'
import { installTDesignIconOfflineGuard } from '../../src/utils/tdesign-icon-offline'
import 'tdesign-vue-next/dist/tdesign.css'
import '../../src/assets/theme/theme.css'
installTDesignIconOfflineGuard()
const app = createApp({ render: () => h(PortalHome) })
const pinia = createPinia()
const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { render: () => null } }] })
app.use(pinia).use(router).use(i18n).use(TDesign).mount('#app')
