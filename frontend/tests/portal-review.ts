// Explicit isolated browser harness. Not included in production build inputs.
// Uses real components and HTTP APIs. Authentication is a real existing JWT
// issued for synthetic fixture records; the SID login ceremony is not mocked.
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import TDesign from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import { createRouter, createMemoryHistory } from 'vue-router'
import i18n from '../src/i18n'
import { useAuthStore } from '../src/stores/auth'
import Review from '../src/components/portal/PortalPublicKnowledge.vue'
import Keys from '../src/views/integrations/MCPAccessKeys.vue'
const mode = new URLSearchParams(location.search).get('mode') || 'owner'
const pinia = createPinia()
const auth = useAuthStore(pinia)
auth.user = { id: 'A', tenant_id: 1, username: 'synthetic-user' } as typeof auth.user
const app = createApp({ render: () => mode === 'keys' ? h(Keys) : h(Review, { tenantId: 99, ownerView: mode === 'owner' }) })
app.use(pinia).use(TDesign).use(i18n).use(createRouter({ history: createMemoryHistory(), routes: [] })).mount('#app')
