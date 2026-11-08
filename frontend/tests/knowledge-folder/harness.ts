import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory } from 'vue-router'
import useKnowledgeBase from '../../src/hooks/useKnowledgeBase'

const app = createApp({
  setup() {
    const state = useKnowledgeBase('folder-regression')
    Object.assign(window, { loadFolder: state.getKnowled })
    return () => h('main', [
      h('div', { 'data-testid': 'error' }, state.knowledgeListError.value || ''),
      h('div', { 'data-testid': 'count' }, String(state.total.value)),
      h('div', { 'data-testid': 'rows' }, String(state.cardList.value.length)),
    ])
  },
})
app.use(createPinia())
app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: {} }))
app.use(createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { render: () => null } }] }))
app.mount('#app')
