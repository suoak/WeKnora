<template>
  <section class="quick-import">
    <div class="connection-row">
      <span>飞书账号</span>
      <div class="connection-actions">
        <t-tag v-if="connected" theme="success" variant="light">飞书已连接</t-tag>
        <template v-else-if="connectionState === 'unknown'"><span>暂时无法检查连接状态</span><t-button variant="text" :loading="checking" @click="refreshConnection">重试</t-button></template>
        <template v-else-if="connectionState === 'reauthorize'"><span>飞书授权已失效</span><t-button :disabled="busy" @click="connect">重新连接</t-button></template>
        <t-button v-else :disabled="busy" @click="connect">连接飞书</t-button>
        <t-dropdown v-if="connected" :options="[{ content: '解除连接', onClick: () => { disconnectConfirm = true } }]">
          <t-button variant="text" aria-label="飞书账号管理">···</t-button>
        </t-dropdown>
      </div>
    </div>
    <t-dialog v-if="disconnectConfirm" :visible="disconnectConfirm" header="解除飞书连接" body="解除后将停止后续导入和自动更新，已导入的文档不会自动删除。" :confirm-btn="{ content: '解除连接', theme: 'danger', loading: checking }" @confirm="disconnect" @close="disconnectConfirm = false" @cancel="disconnectConfirm = false" />
    <label for="feishu-target">保存到知识库</label>
    <t-select id="feishu-target" v-model="targetID" :options="targets.map(row => ({label:row.name,value:row.id}))" :disabled="busy || !!kbId" placeholder="请选择已有知识库" />
    <p v-if="targetsLoaded && !targets.length" class="warning">暂无可写知识库，请先创建或申请访问知识库。<router-link to="/platform/knowledge-bases">前往知识库</router-link></p>
    <p v-if="kbId && targetsLoaded && !target" class="warning">当前知识库不可写，请申请访问权限。</p>
    <label for="feishu-links">飞书文档链接</label>
    <t-textarea id="feishu-links" v-model="linksText" :disabled="busy" :autosize="{ minRows: 5, maxRows: 8 }" placeholder="每行一个链接，支持 Wiki、Docx、Sheet 和 Bitable，最多 100 篇" />
    <p v-if="input.invalid.length" class="warning">{{ input.invalid.length }} 条链接格式有误，将跳过；其余链接仍可导入。</p>
    <details v-if="input.invalid.length"><summary>查看错误链接</summary><ul><li v-for="url in input.invalid" :key="url">{{ url }}</li></ul></details>
    <div class="actions">
      <t-button :loading="busy" :disabled="!connected || !target || !input.valid.length || input.valid.length > 100 || accepted" @click="submit">一键导入<span v-if="input.valid.length">（{{ input.valid.length }}）</span></t-button>
      <t-button variant="text" :disabled="!connected || busy" @click="showBrowser = !showBrowser; showBrowser && browse()">从飞书选择文档</t-button>
    </div>
    <section v-if="showBrowser" class="browser" aria-label="从飞书选择文档">
      <div class="actions"><t-select v-model="kind" :options="[{label:'我的空间',value:'drive'},{label:'Wiki 空间',value:'wiki'}]" :disabled="busy" @change="resetBrowser" /><t-button v-if="trail.length" variant="text" :disabled="busy" @click="back">上一级</t-button></div>
      <div v-for="entry in entries" :key="entry.id" class="resource">
        <t-checkbox v-if="entry.url && !['folder','wiki_space'].includes(entry.type)" :checked="selected(entry.url)" :disabled="busy" @change="choose(entry.url!)">{{ entry.name }}</t-checkbox>
        <span v-else>{{ entry.name }}</span>
        <t-button v-if="entry.has_children" variant="text" :disabled="busy" @click="open(entry)">打开</t-button>
      </div>
      <t-button v-if="cursor" variant="text" :disabled="busy" @click="browse(true)">加载更多</t-button>
    </section>
    <p v-if="message" role="status">{{ message }}</p>
    <router-link v-if="recent" :to="{path:'/platform/feishu/imports',query:{kb:recent.kb, batch:recent.id}}">查看最近导入任务</router-link>
    <router-link v-else to="/platform/feishu/imports">导入历史与设置</router-link>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onUnmounted, onMounted } from 'vue'
import { feishuStatus, disconnectFeishu, feishuImportTargets, quickImportFeishu, feishuResources, type FeishuStatus, type FeishuResource, type FeishuImportTarget } from '@/api/feishuPersonal'
import { normalizeFeishuLinks } from '@/utils/feishuQuickImport'
import { feishuBatchKey } from '@/utils/feishuBatchKey'
import { FEISHU_TARGET } from '@/utils/feishuOAuthFlow'
const props = defineProps<{ kbId: string; status: FeishuStatus; initialUrl?: string }>()
const emit = defineEmits<{ connect: [links: string]; imported: [] }>()
const linksText = ref(props.initialUrl || ''), connected = ref(props.status.connected), busy = ref(false), message = ref(''), accepted = ref(false)
const connectionState = ref(props.status.connected ? 'connected' : 'disconnected'), checking = ref(false), disconnectConfirm = ref(false)
const input = computed(() => normalizeFeishuLinks(linksText.value))
const targets = ref<FeishuImportTarget[]>([]), targetsLoaded = ref(false), targetID = ref(props.kbId)
try { if (!props.kbId) { targetID.value = sessionStorage.getItem(FEISHU_TARGET) || ''; sessionStorage.removeItem(FEISHU_TARGET) } } catch { /* Optional draft only. */ }
const target = computed(() => targets.value.find(row => row.id === targetID.value))
feishuImportTargets().then(rows => { if (mounted) { targets.value = rows; targetsLoaded.value = true } }).catch(() => { if (mounted) message.value = '无法读取可写知识库，请重新打开导入窗口。' })
const recent = ref<{id:string;kb:string}>(), showBrowser = ref(false), kind = ref('drive')
const entries = ref<FeishuResource[]>([]), cursor = ref(''), location = ref({parent:'',space:''}), trail = ref<{parent:string;space:string}[]>([])
let mounted = true, epoch = 0, key = ''
let connectionTimer: ReturnType<typeof setInterval> | undefined, lastCheck = 0
onMounted(() => { window.addEventListener('focus', onFocus); connectionTimer = setInterval(refreshConnection, 5 * 60 * 1000) })
onUnmounted(() => { mounted = false; epoch++; clearInterval(connectionTimer); window.removeEventListener('focus', onFocus) })
watch(linksText, () => { key = ''; accepted.value = false; message.value = '' })
watch(targetID, () => { key = ''; accepted.value = false })
watch(() => props.initialUrl, value => { linksText.value = value || '' })
watch(() => props.status, () => { void refreshConnection() }, {immediate:true,deep:true})
function onFocus() { if (Date.now() - lastCheck > 10000) void refreshConnection() }
async function refreshConnection() {
 if (checking.value) return
 checking.value = true; lastCheck = Date.now()
 try { const status = await feishuStatus(); if (mounted) { connected.value = status.connected; connectionState.value = status.connection_state || (status.connected ? 'connected' : 'disconnected') } }
 catch { if (mounted) { connected.value = false; connectionState.value = 'unknown' } }
 finally { checking.value = false }
}
async function disconnect() {
 if (checking.value) return
 checking.value = true
 try { await disconnectFeishu(); if (mounted) { connected.value = false; connectionState.value = 'disconnected'; disconnectConfirm.value = false } }
 catch { if (mounted) message.value = '暂时无法解除连接，请稍后重试。' }
 finally { checking.value = false }
}
async function submit() {
 if (busy.value || accepted.value || !target.value || !input.value.valid.length || input.value.valid.length > 100) return
 busy.value = true; message.value = ''
 if (!key) key = feishuBatchKey()
 try {
  const result = await quickImportFeishu(input.value.valid, key, targetID.value)
  if (!mounted) return
  recent.value = {id:result.batch.id,kb:result.batch.kb_id}; accepted.value = true
  message.value = `已添加 ${result.items.length} 篇文档到导入队列，系统将在后台自动处理。可以关闭页面。`
  emit('imported')
 } catch { if (mounted) message.value = '提交暂未确认，请稍后再次点击；重复提交不会重复导入。若授权已失效，请重新连接飞书。' }
 finally { busy.value = false }
}
function connect() {
 try { sessionStorage.setItem(FEISHU_TARGET,targetID.value) } catch { /* Optional draft only. */ }
 emit('connect',linksText.value)
}
function selected(url: string) { return input.value.valid.includes(normalizeFeishuLinks(url).valid[0]) }
function choose(url: string) { const normalized = normalizeFeishuLinks(url).valid[0]; if (!normalized) return; const found = new Set(input.value.valid); if (found.has(normalized)) found.delete(normalized); else found.add(normalized); linksText.value = [...found,...input.value.invalid].join('\n') }
async function browse(append = false) {
 if (busy.value) return
 busy.value = true; const version = epoch, position = {...location.value}, sourceKind = kind.value
 try { const page = await feishuResources({auth_mode:'user_oauth',kind:sourceKind,parent_id:position.parent,space_id:position.space,cursor:append?cursor.value:'',page_size:50}); if (mounted && epoch === version) { entries.value = append ? [...entries.value,...page.items] : page.items; cursor.value = page.cursor || '' } }
 catch { if (mounted) message.value = '暂时无法浏览文档，仍可粘贴链接导入。' } finally { busy.value = false }
}
async function resetBrowser() { epoch++; trail.value = []; location.value = {parent:'',space:''}; entries.value = []; cursor.value = ''; await browse() }
async function open(entry:FeishuResource) { if (busy.value) return; epoch++; trail.value.push({...location.value}); location.value = entry.type === 'wiki_space' ? {parent:'',space:entry.id} : {parent:entry.id,space:entry.space_id || location.value.space}; await browse() }
async function back() { if (busy.value) return; epoch++; location.value = trail.value.pop() || {parent:'',space:''}; await browse() }
</script>
<style scoped>
.quick-import { display:flex; flex-direction:column; gap:16px; min-width:0; max-width:960px; margin:0 auto; padding:8px 0; }
.connection-row { display:flex; align-items:center; justify-content:space-between; gap:12px; padding-bottom:16px; border-bottom:1px solid var(--td-component-border); }
.connection-actions { display:flex; align-items:center; flex-wrap:wrap; gap:8px; }
label { font-weight:500; color:var(--td-text-color-primary); }
.quick-import > :deep(.t-select__wrap), .quick-import > :deep(.t-textarea) { width:100%; }
.actions { margin-top:4px; }
p { margin:0; line-height:1.6; }
.hint { color:var(--td-text-color-secondary); font-size:var(--app-text-sm); }
.warning { color:var(--td-warning-color); }
.account { color:var(--td-success-color); }
.actions,.resource { display:flex; flex-wrap:wrap; align-items:center; gap:8px; }
.actions :deep(.t-select__wrap) { max-width:180px; }
.browser { max-height:260px; overflow:auto; border:1px solid var(--td-component-border); border-radius:8px; padding:12px; }
.resource { justify-content:space-between; padding:6px 0; overflow-wrap:anywhere; }
li { overflow-wrap:anywhere; }
@media(max-width:600px) { .actions > :first-child { width:100%; } }
</style>
