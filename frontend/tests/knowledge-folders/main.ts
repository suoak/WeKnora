import { createApp, h, ref, onMounted } from 'vue'
import TDesign from 'tdesign-vue-next'
import { createPinia } from 'pinia'
import i18n from '../../src/i18n'
import KbFolderTree from '../../src/views/knowledge/components/KbFolderTree.vue'
import CreateDocumentFolderDialog from '../../src/views/knowledge/components/CreateDocumentFolderDialog.vue'
import BatchTagDialog from '../../src/views/knowledge/components/BatchTagDialog.vue'
import AccessRequestsPanel from '../../src/components/portal/AccessRequestsPanel.vue'
import { initFont } from '../../src/composables/useFont'
import FolderPickerMenu from '../../src/views/knowledge/components/FolderPickerMenu.vue'
import { listKnowledgeFolders, renameKnowledgeFolder, deleteEmptyKnowledgeFolder, type KnowledgeFolderTree } from '../../src/api/knowledge-base'
import { installTDesignIconOfflineGuard } from '../../src/utils/tdesign-icon-offline'
import 'tdesign-vue-next/dist/tdesign.css'
import '../../src/assets/theme/theme.css'
import '../../src/assets/theme/tdesign-overrides.less'
import './preview.css'

installTDesignIconOfflineGuard()
initFont()
const tree = ref<KnowledgeFolderTree | null>(null)
const params = new URLSearchParams(location.search)
const selected = ref(params.get('parent') || '')
const parent = ref('')
const visible = ref(false)
const viewer = new URLSearchParams(location.search).has('viewer')
const refresh = async () => { const res: any = await listKnowledgeFolders('kb-1'); tree.value = res.data }
const paths = () => {
 const result: string[] = []
 const walk = (nodes: KnowledgeFolderTree['folders']) => nodes.forEach(n => { result.push(n.path); walk(n.children || []) })
 walk(tree.value?.folders || []); return result
}
createApp({
 setup() {
  onMounted(() => { if (!params.has('requests')) void refresh() })
  return () => params.has('requests')
   ? h('main', { style:'padding:32px;max-width:900px' }, [h(AccessRequestsPanel, {tenantId:7})])
   : h('main', { style: 'padding:32px;max-width:1100px' }, [
   h('h1', 'KnowHub 多级空目录验收'),
   h('p', '实际目录 API · 磁盘持久化 · 无占位文档'),
   h('div', { style: 'display:flex;gap:24px;min-height:420px' }, [
    h(KbFolderTree, { tree:tree.value, selectedPath:selected.value, rootLabel:'空知识库', canEdit:!viewer,
     onSelect:(p:string)=>selected.value=p,
     onCreate:(p:string)=>{parent.value=p;visible.value=true},
     onRename:async ({from,to}:{from:string;to:string})=>{await renameKnowledgeFolder('kb-1',from,to);await refresh()},
     'onDelete-empty':async (p:string)=>{await deleteEmptyKnowledgeFolder('kb-1',p);await refresh()},
    }),
    h('section', [h('h2',selected.value || '根目录'),h('p',`文档总数：${tree.value?.total_document_count ?? 0}`),
     !viewer && h('button', { onClick:()=>{parent.value=selected.value;visible.value=true} }, '新建文件夹'),
     !viewer && h('details', [h('summary','移动目标选择器'),h(FolderPickerMenu, { kbId:'kb-1', options:paths().map(path=>({path,name:path.split('/').at(-1)!,depth:path.split('/').length-1})), onCreate:refresh })]),
    ]),
   ]),
   params.has('reference')
     ? h(BatchTagDialog, { visible:visible.value, 'onUpdate:visible':(v:boolean)=>visible.value=v, kbId:'kb-1', count:2 })
     : h(CreateDocumentFolderDialog, { visible:visible.value, 'onUpdate:visible':(v:boolean)=>visible.value=v, kbId:'kb-1',kbName:params.get('kbName') || '空知识库',parentPath:parent.value,canEdit:!viewer,folderPaths:paths(),afterCreate:refresh }),
  ])
 }
}).use(createPinia()).use(i18n).use(TDesign).mount('#app')
