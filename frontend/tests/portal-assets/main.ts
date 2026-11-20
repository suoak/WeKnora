import { createApp, h, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, RouterView } from 'vue-router'
import i18n from '../../src/i18n'
import PortalHome from '../../src/views/portal/PortalHome.vue'
import DocumentScopeSummary from '../../src/views/knowledge/components/DocumentScopeSummary.vue'
import { usePortalStore } from '../../src/stores/portal'
import { useAuthStore } from '../../src/stores/auth'
import { invalidatePortalAssets, resetPortalSession } from '../../src/stores/portalEvents'

const pinia = createPinia()
setActivePinia(pinia)
const scope = ref({ count: 0, wholeCount: 339, filtered: false })
const router = createRouter({ history: createMemoryHistory(), routes: [
  { path: '/portal', component: PortalHome },
  { path: '/knowledge', component: { setup: () => () => h(DocumentScopeSummary, scope.value) } },
] })
const app = createApp({ render: () => h(RouterView) })
app.use(pinia).use(i18n).use(router)
const auth = useAuthStore()
auth.setToken('synthetic-browser-only')
auth.setUser({ id: 'user-a', username: 'browser fixture', tenant_id: 1, is_active: true, is_system_admin: true } as any)
auth.setTenant({ id: 1, name: 'fixture', status: 'active' } as any)
app.mount('#app')
const portal = usePortalStore()
Object.assign(window, { portalHarness: { portal, auth, router, invalidatePortalAssets, resetPortalSession, scope } })
void router.push('/portal')
