import { createApp, defineComponent, h } from 'vue';
import { createI18n } from 'vue-i18n';
import Notice from '../../src/views/knowledge/components/SourceRestrictionNotice.vue';
import Menu from '../../src/views/knowledge/components/DocumentActionMenu.vue';
import zh from '../../src/i18n/locales/zh-CN';
import en from '../../src/i18n/locales/en-US';
import type { SourceRestriction } from '../../src/api/sourceRestriction';

document.body.dataset.rebuildLabel = zh.knowledgeBase.rebuildDocument;
const state = new URLSearchParams(location.search).get('state') || 'hold';
const restricted = state === 'hold' || state === 'revoked';
const status: SourceRestriction | null = state === 'unknown' ? null : {
  restricted, source_state: state as SourceRestriction['source_state'], historical_processing_state: 'unverified',
};
const app = createApp(defineComponent({ setup: () => () => h('main', [
  h(Notice, { status, unavailable: state === 'unknown', hasPending: true }),
  h(Menu, {
    item: { id: 'isolated-document', type: 'file', parse_status: 'pending', file_name: 'test.pdf' },
    canDownload: false, canMutateKnowledge: false, traceVisible: false, sourceRestricted: restricted,
    onReparse: () => { document.body.dataset.reparse = 'emitted'; },
  }),
]) }));
app.use(createI18n({ legacy: false, locale: 'zh-CN', fallbackLocale: 'en-US', messages: { 'zh-CN': zh, 'en-US': en } }));
// UI primitives only: the production notice/menu are compiled without copies.
app.component('t-icon', defineComponent({ render: () => h('span') }));
app.component('t-popconfirm', defineComponent({ setup: (_, { slots }) => () => h('div', slots.default?.()) }));
app.mount('#app');
