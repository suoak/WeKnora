// Compiled only by the opt-in isolated QA build, never by the product build.
// Authentication is supplied by the API-intercepting Playwright fixture, not this entry.
if (import.meta.env.MODE != 'rc24-qa' || location.hostname != '127.0.0.1'
    || localStorage.getItem('weknora_token') != 'local-fixture-token') {
  throw new Error('Editor QA entry requires the isolated fixture and rc24-qa build mode')
}

import { createApp, defineComponent, h, nextTick, ref } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import TDesign from 'tdesign-vue-next'
import 'tdesign-vue-next/es/style/index.css'
import i18n from '../../src/i18n'
import AgentEditorModal from '../../src/views/agent/AgentEditorModal.vue'
import AgentEditorPanel from '../../src/views/agent/AgentEditorPanel.vue'
import { useAuthStore } from '../../src/stores/auth'

const visible = ref(false), mode = ref<'create' | 'edit'>('edit'), agent = ref<any>(null)
const boundaryTest = ref(false), broken = ref(true)
const ThrowingChild = defineComponent({ setup: () => () => {
  if (broken.value) throw new TypeError('isolated model child failure')
  return h('p', 'Recovered child')
} })
const app = createApp({ setup: () => () => boundaryTest.value
  ? h('div', [h(AgentEditorPanel, { title: '模型配置', onRetry: () => broken.value = false }, { default: () => h(ThrowingChild) }), h('p', 'Other panel remains available')])
  : h(AgentEditorModal, { visible: visible.value, mode: mode.value, agent: agent.value, 'onUpdate:visible': (value: boolean) => visible.value = value }) })
app.use(createPinia()).use(i18n).use(TDesign).use(createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { render: () => null } }] }))
app.config.errorHandler = (error) => { console.error('Unhandled component error', error) }
useAuthStore().initFromStorage()
app.mount('#app')
;(window as any).editorHarness = {
  async open(value: any, editorMode = 'edit') {
    visible.value = false; await nextTick(); agent.value = value; mode.value = editorMode as any; visible.value = true
  },
  boundary() { boundaryTest.value = true },
}
