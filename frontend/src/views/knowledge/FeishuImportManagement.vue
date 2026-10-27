<template>
  <main class="management">
    <h2>飞书导入历史</h2>
    <p><router-link to="/platform/knowledge-bases">返回知识库</router-link></p>
    <p>任务在后台持续执行。可以关闭页面，稍后再查看。</p>
    <p v-if="message" role="status">{{ message }}</p>
    <template v-if="status">
      <div v-for="row in history" :key="row.id" class="history-row">
        <div class="history-summary"><strong>{{ row.document_names?.length ? row.document_names.join('、') : '飞书文档导入' }}</strong><span>{{ row.target_name || '知识库' }} · {{ new Date(row.created_at).toLocaleString() }}</span><t-tag :theme="row.status === 'succeeded' ? 'success' : row.status === 'failed' ? 'danger' : 'primary'" variant="light">{{ row.status === 'succeeded' ? '导入成功' : row.status === 'failed' ? '失败' : '处理中' }}</t-tag></div>
        <t-button variant="text" @click="showBatch(row.id)">查看进度</t-button>
      </div>
      <p v-if="!history.length">暂无导入记录。</p>
      <t-button v-if="more" variant="text" @click="loadHistory(true)">加载更多</t-button>
      <section v-if="batch" aria-label="导入进度">
        <h3>{{ batch.target_name || '导入进度' }} · {{ simpleStatus(batch.status) }}</h3>
        <article v-for="item in batch.items" :key="item.id" class="item">
          <p><strong>{{ item.document_name || `第 ${item.ordinal + 1} 篇文档` }}</strong><t-tag size="small" variant="light">{{ item.resource_type || '文档' }}</t-tag><span>{{ itemStatus(item) }}</span></p>
          <p v-if="item.stalled" class="warning" role="status">处理时间较长，请稍后查看或联系管理员。</p>
          <p v-if="item.error_category" class="warning">{{ failure(item.error_category) }}</p>
          <t-button v-if="['user_scope_missing','reconnect_required','authorization_expired'].includes(item.error_category || '')" variant="text" :loading="authorizationBusy" @click="connect('')">补充飞书授权</t-button>
          <router-link v-if="item.status === 'succeeded' && item.index_verified && item.knowledge_id" :to="{path:'/platform/knowledge-bases/'+batch.batch.kb_id,query:{knowledge_id:item.knowledge_id}}">在知识库中查看</router-link>
        </article>
        <t-button v-if="batch.items.some(item => ['failed','blocked'].includes(item.status))" variant="outline" :loading="retryBusy" @click="retry">重新尝试</t-button>
      </section>
    </template>
    <p v-else>正在读取导入记录。</p>
  </main>
</template>
<script setup lang="ts">
import {computed,ref,onMounted,onUnmounted} from 'vue'
import {useRoute} from 'vue-router'
import {feishuStatus,authorizeFeishu,listFeishuBatches,listMyFeishuBatches,getFeishuBatch,retryFeishuBatch,type FeishuStatus,type FeishuBatch,type FeishuBatchHistory,type FeishuBatchItem} from '@/api/feishuPersonal'
import {saveFeishuDraft,takeFeishuDraft} from '@/utils/feishuOAuthFlow'
const route=useRoute(), kb=typeof route.query.kb === 'string' ? route.query.kb : '', status=ref<FeishuStatus>()
const history=ref<FeishuBatchHistory[]>([]), batch=ref<FeishuBatch>(), more=ref(false), message=ref('')
const approvalKB=computed(()=>kb || batch.value?.batch.kb_id || ''), retryBusy=ref(false)
const authorizationBusy=ref(false)
let mounted=true, selectedID='', timer:ReturnType<typeof setTimeout>|undefined, loading=false
onUnmounted(()=>{mounted=false;clearTimeout(timer)})
function formatTime(value:string) { return new Date(value).toLocaleString() }
function itemStatus(item:FeishuBatchItem) {
  if(item.status==='succeeded' && !item.index_verified) return '等待索引验证'
  if(item.stage==='publication_pending') return '解析已完成，正在验证索引和发布'
  if(item.stage==='wiki_finishing') return '索引已验证，正在完成知识发布'
  if(item.status==='processing') return '正在检查和准备'
  if(item.status!=='dispatched') return simpleStatus(item.status)
  const labels:Record<string,string>={queued:'排队中',checking:'正在检查和准备',preparing:'正在准备文档',fetching:'正在读取飞书文档',retry_wait:'等待自动重试',downloaded:'文档已读取，等待解析',parse_queued:'等待解析',parsing:'正在解析',docreader:'正在解析',chunking:'正在切分文档',embedding:'正在建立索引',multimodal:'正在处理图片',postprocess:'正在完成发布',finalizing:'正在完成发布',completed:'已验证成功入库',needs_attention:'需要处理'}
  return labels[item.stage || ''] || '任务已交接，等待后台状态'
}
function failure(code:string) {
	if(code==='source_identity_rebind_required') return '飞书授权已更新，请联系管理员更新此来源的授权绑定。'
	if(code==='source_copy_export_denied') return '当前账号可以阅读该文档，但没有复制或导出权限。请联系文档所有者开放相应权限，无需申请编辑权限。'
	if(code==='source_read_denied') return '当前账号没有文档阅读权限，请联系文档所有者。'
	if(code==='source_copy_denied') return '当前账号没有文档复制权限，请联系文档所有者。'
	if(code==='source_copy_only_requires_policy') return '文档允许复制但导出受限，暂不能导入，请联系管理员。'
	if(code==='source_acquisition_requires_review') return '文档获取方式需要核查，请联系管理员。'
	if(code==='source_policy_verification_required') return '无法确认文档导出权限，请联系管理员。'
	if(code==='blocks_content_requires_review') return '文档含暂不支持的内容，请联系管理员处理。'
  if(['authorization_expired','reconnect_required'].includes(code)) return '飞书授权已失效，请重新连接账号。'
  if(code==='export_permission_required') return '缺少导出权限，请联系文档所有者。'
  if(code==='source_permission_required') return '无法读取文档，请联系文档所有者。'
  if(code==='source_metadata_denied') return '无法读取文档信息，请联系文档所有者。'
  if(code==='export_create_denied') return '飞书拒绝导出，请联系文档所有者检查导出权限。'
  if(code==='export_status_denied') return '无法获取导出结果，请检查飞书账号和文档权限。'
  if(code==='export_download_denied') return '飞书拒绝下载导出文件，请联系文档所有者。'
  if(code==='user_scope_missing') return '飞书授权缺少所需权限，请重新授权。'
  if(code==='app_scope_missing' || code==='oauth_app_configuration') return '飞书应用权限或配置有误，请联系管理员。'
  if(code==='refresh_token_expired') return '飞书授权已到期，请重新连接。'
  if(code==='refresh_rejected') return '飞书授权无法续期，请重新连接。'
  if(code==='authorization_invalid') return '飞书连接无法验证，请重新连接。'
  if(code==='sharing_approval_required') return '请点击重新尝试，系统将重新检查文档和知识库权限。'
  if(code==='publication_confirmation_expired') return '请点击重新尝试。'
  if(code==='kb_permission_required') return '目标知识库不可写，请申请访问权限。'
  if(code==='api_scope_missing') return '飞书应用缺少所需权限，请联系管理员。'
  if(code==='parse_failed') return '文档解析失败，请重新尝试或联系管理员。'
  if(code==='index_failed') return '文档索引失败，请重新尝试或联系管理员。'
  const labels:Record<string,string>={storage_permission_denied:'文件暂存目录不可写，请联系管理员。',storage_unavailable:'文件暂存服务不可用，请联系管理员。',authorization_revalidation_failed:'授权检查未通过，请重新连接飞书并检查文档权限。',candidate_content_incomplete:'文档读取不完整，请检查导出权限。',source_version_changed:'文档版本已变化，需要重新检查。',temporary_failure:'服务暂时不可用，系统将有限重试。',retry_exhausted:'自动重试次数已用完，请重新尝试或联系管理员。',parse_or_index_failed:'解析或索引失败，请重新尝试或联系管理员。',candidate_requires_review:'文档处理未完成，请联系管理员查看详情。'}
  return labels[code] || '导入未完成，请重新尝试或联系管理员。'
}
function simpleStatus(state:string) {
  if (state==='succeeded') return '导入成功'
  if (['existing','duplicate'].includes(state)) return '已去重，等待验证'
  if (['failed','blocked','partial_failed','needs_attention'].includes(state)) return '失败'
  if (state==='processing') return '正在检查和准备'
  if (['dispatched','running'].includes(state)) return '处理中'
  return '排队中'
}
onMounted(async()=>{
  takeFeishuDraft(location.pathname+location.search)
  try {
    status.value=await feishuStatus()
    await loadHistory(false)
    if(typeof route.query.batch==='string') await showBatch(route.query.batch)
    else if(history.value.length) await showBatch(history.value[0].id)
  } catch {message.value='暂时无法读取导入记录，请稍后刷新。'}
})
async function loadHistory(append:boolean) {
  try {const offset=append?history.value.length:0; const rows=await (kb?listFeishuBatches(kb,offset):listMyFeishuBatches(offset)); if(!mounted)return; history.value=append?[...history.value,...rows]:rows; more.value=rows.length===20}
  catch {if(mounted)message.value='暂时无法读取导入记录，请稍后刷新。'}
}
async function showBatch(id:string) {
  selectedID=id; clearTimeout(timer)
  if(loading) { if(mounted)timer=setTimeout(()=>showBatch(selectedID),1000); return }
  loading=true
  try {const row=await getFeishuBatch(id); if(mounted && selectedID===id && (!kb || row.batch.kb_id===kb)){batch.value=row;message.value=''}}
  catch {if(mounted)message.value='暂时无法读取任务，请稍后刷新。'}
  finally {
    loading=false
    if(mounted && selectedID && (!batch.value || batch.value.batch.id!==selectedID || batch.value.items.some(item=>['pending','processing','dispatched'].includes(item.status))))timer=setTimeout(()=>showBatch(selectedID),4000)
  }
}
async function retry() {
  if(!batch.value || retryBusy.value)return
  retryBusy.value=true
  try {await retryFeishuBatch(batch.value.batch.id);await showBatch(batch.value.batch.id)}
  catch {message.value='暂时无法重试，请检查账号和知识库权限。'}
  finally {retryBusy.value=false}
}
async function connect(links:string) {
  if(authorizationBusy.value)return
  authorizationBusy.value=true
  try {
    const before=await feishuStatus()
    saveFeishuDraft(approvalKB.value,links,location.pathname+location.search,before.authorization_revision || '')
    const result=await authorizeFeishu(), target=new URL(result.authorization_url)
    if(target.origin!=='https://accounts.feishu.cn')throw new Error('invalid_authorization_origin')
    location.assign(target.href)
  } catch { message.value='暂时无法连接飞书，请稍后重试。' }
  finally { authorizationBusy.value=false }
}
</script>
<style scoped>
.management {max-width:960px;margin:32px auto;padding:0 24px 40px;overflow-wrap:anywhere;}
.history-summary {display:flex;flex-direction:column;align-items:flex-start;gap:8px;min-width:0;}
.history-summary span {font-size:13px;color:var(--td-text-color-secondary);}
.history-row,.item {padding:20px;border:1px solid var(--td-component-border);border-radius:10px;background:var(--td-bg-color-container);margin:12px 0;}
.item > p:first-child {display:flex;flex-wrap:wrap;align-items:center;gap:12px;}
@media(max-width:600px){.management{padding:0 16px}.history-row{align-items:flex-start}.item{padding:16px}}
.history-row {display:flex;justify-content:space-between;align-items:center;gap:16px;}
details {margin-top:24px;}
summary {cursor:pointer;}
.item {padding:12px 0;border-bottom:1px solid var(--td-component-border);}
.warning {color:var(--td-warning-color);}
.continue {padding:12px;border:1px solid var(--td-component-border);border-radius:8px;line-height:1.6;}
</style>
