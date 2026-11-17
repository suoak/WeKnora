import { createApp, h, nextTick, ref } from 'vue'
import TDesign from 'tdesign-vue-next'
import { createPinia } from 'pinia'
import i18n from '../../src/i18n'
import DataSourceEditorDialog from '../../src/views/knowledge/settings/DataSourceEditorDialog.vue'
import { installTDesignIconOfflineGuard } from '../../src/utils/tdesign-icon-offline'
import 'tdesign-vue-next/dist/tdesign.css'
import '../../src/assets/theme/theme.css'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'

installTDesignIconOfflineGuard()
const visible = ref(false)
const selected = new URLSearchParams(location.search).get('selected')
const system = new URLSearchParams(location.search).has('system')
const create = new URLSearchParams(location.search).has('create')
const source = {
  id: 'fixture-source', name: 'Confluence', type: 'confluence', tenant_id: 1, knowledge_base_id: 'fixture-kb',
  credentials: { credentials: { configured: true } },
  config: { settings: system ? { credential_source: 'system' } : {}, resource_ids: selected ? [selected] : [] },
  sync_schedule: '0 0 */6 * * *', sync_mode: 'incremental', conflict_strategy: 'overwrite', sync_deletions: true,
}
const app = createApp({ render: () => h(DataSourceEditorDialog, {
  visible: visible.value, 'onUpdate:visible': (value: boolean) => { visible.value = value },
  kbId: 'fixture-kb', dataSource: create ? null : source as any,
}) })
app.use(createPinia()).use(i18n).use(TDesign).mount('#app')
void nextTick(() => { visible.value = true })
