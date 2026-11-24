import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'
import i18n from '../../src/i18n'
import Picker from '../../src/views/knowledge/components/KbUploadSourceDropdown.vue'
import { uploadKnowledgeFile } from '../../src/api/knowledge-base'
import 'tdesign-vue-next/es/style/index.css'

const selected = ref<File[]>([]), status = ref('等待选择')
createApp({ setup: () => () => h('main', [
  h(Picker, { supportedFileTypes: ['docx', 'xlsx', 'xlsm', 'pptx', 'doc', 'xls', 'ppt'],
    onFiles: (files: File[]) => { selected.value = files; status.value = files.map(f => f.name).join(',') } }),
  h('output', { id: 'status' }, status.value),
  h('button', { id: 'send', onClick: async () => {
    for (const file of selected.value) await uploadKnowledgeFile('synthetic-kb', { file })
    status.value = '上传完成'
  } }, '上传'),
]) }).use(createPinia()).use(i18n).mount('#app')

// Exercise the actual API wrapper independently of the selection component.
Object.assign(window, { uploadOfficeTest: async (bytes: number[], name: string) => {
  try { await uploadKnowledgeFile('synthetic-kb', { file: new File([new Uint8Array(bytes)], name) }); return 'sent' }
  catch (e) { return (e as { code?: number }).code }
} })
