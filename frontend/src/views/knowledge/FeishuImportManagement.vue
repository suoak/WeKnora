<template>
  <main class="management">
    <h2>飞书导入历史</h2>
    <p><router-link to="/platform/knowledge-bases">返回知识库</router-link></p>
    <p>任务在后台持续执行。可以关闭页面，稍后再查看。</p>
    <p v-if="message" role="status">{{ message }}</p>
    <template v-if="status">
      <div v-for="row in history" :key="row.id" class="history-row">
        <span>{{ new Date(row.created_at).toLocaleString() }}</span>
        <t-button variant="text" @click="showBatch(row.id)">查看任务</t-button>
      </div>
      <p v-if="!history.length">暂无导入记录。</p>
      <t-button v-if="more" variant="text" @click="loadHistory(true)">加载更多</t-button>
      <section v-if="batch" aria-label="导入进度">
        <h3>{{ simpleStatus(batch.status) }}</h3>
        <article v-for="item in batch.items" :key="item.id" class="item">
          <p>第 {{ item.ordinal + 1 }} 篇：{{ itemStatus(item) }}</p>
          <p v-if="item.last_progress_at">最后推进：{{ formatTime(item.last_progress_at) }}</p>
          <p v-if="item.last_activity_at">后台状态更新：{{ formatTime(item.last_activity_at) }}</p>
          <p v-if="item.stalled" class="warning" role="status">处理异常，请查看详情。后台正在检查任务状态，不会擅自取消任务。</p>
          <p v-if="item.error_category" class="warning">{{ failure(item.error_category) }}</p>
          <p v-if="item.retry_after && item.status === 'dispatched'">自动重试时间：{{ formatTime(item.retry_after) }}</p>
          <p v-if="item.chunk_count || item.vector_count">已解析 {{ item.chunk_count || 0 }} 个片段，已建立 {{ item.vector_count || 0 }} 条向量索引。</p>
          <router-link v-if="item.status === 'succeeded' && item.index_verified && item.knowledge_id" :to="{path:'/platform/knowledge-bases/'+batch.batch.kb_id,query:{knowledge_id:item.knowledge_id}}">在知识库中查看</router-link>
          <details v-if="item.error_category"><summary>高级详情</summary><p>原因码：{{ item.error_category }}；自动重试：{{ item.attempt_retry_count || 0 }} 次</p></details>
        </article>
        <t-button variant="text" @click="showBatch(batch.batch.id)">刷新状态</t-button>
      </section>
      <details v-if="kb"><summary>高级设置与安全详情</summary><FeishuAccessManagement :kb-id="kb" :status="status" @connect="connect" /></details>
    </template>
    <p v-else>正在读取导入记录。</p>
  </main>
</template>
<script setup lang="ts">
import {ref,onMounted,onUnmounted} from 'vue'
import {useRoute} from 'vue-router'
import {feishuStatus,authorizeFeishu,listFeishuBatches,listMyFeishuBatches,getFeishuBatch,type FeishuStatus,type FeishuBatch,type FeishuBatchHistory,type FeishuBatchItem} from '@/api/feishuPersonal'
import {FEISHU_PENDING} from '@/i18n/feishuPersonal'
import FeishuAccessManagement from './components/FeishuAccessManagement.vue'
const route=useRoute(), kb=typeof route.query.kb === 'string' ? route.query.kb : '', status=ref<FeishuStatus>()
const history=ref<FeishuBatchHistory[]>([]), batch=ref<FeishuBatch>(), more=ref(false), message=ref('')
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
  if(['authorization_expired','reconnect_required'].includes(code)) return '飞书授权已失效，请重新连接账号。'
  if(['export_permission_required','source_permission_required'].includes(code)) return '缺少文档读取或导出权限，请联系文档所有者。'
  if(code==='sharing_approval_required') return '缺少当前版本的共享发布批准，请在高级设置中处理。'
  if(code==='kb_permission_required') return '目标知识库不可写，请申请访问权限。'
  if(code==='api_scope_missing') return '飞书应用缺少所需权限，请联系管理员。'
  if(code==='parse_failed') return '文档解析失败，请查看详情。'
  if(code==='index_failed') return '文档索引失败，请查看详情。'
  const labels:Record<string,string>={storage_permission_denied:'文件暂存目录不可写，请联系管理员。',storage_unavailable:'文件暂存服务不可用，请联系管理员。',authorization_revalidation_failed:'授权检查未通过，请重新连接飞书并检查文档权限。',candidate_content_incomplete:'文档读取不完整，请检查导出权限。',source_version_changed:'文档版本已变化，需要重新检查。',temporary_failure:'服务暂时不可用，系统将有限重试。',retry_exhausted:'自动重试次数已用完，请查看详情。',parse_or_index_failed:'解析或索引失败，请查看详情。',candidate_requires_review:'文档处理未完成，请联系管理员查看详情。'}
  return labels[code] || '文档需要处理，请在高级详情中查看原因。'
}
function simpleStatus(state:string) {
  if (state==='succeeded') return '已验证成功入库'
  if (['existing','duplicate'].includes(state)) return '已去重，等待验证'
  if (['failed','blocked','partial_failed','needs_attention'].includes(state)) return '需要处理'
  if (state==='processing') return '正在检查和准备'
  if (['dispatched','running'].includes(state)) return '后台处理中，详情见各文档阶段'
  return '排队中'
}
onMounted(async()=>{
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
async function connect(links:string) { sessionStorage.setItem(FEISHU_PENDING,JSON.stringify({kbId:kb,url:links,path:location.pathname+location.search})); const result=await authorizeFeishu(), target=new URL(result.authorization_url); if(target.origin==='https://accounts.feishu.cn') location.assign(target.href) }
</script>
<style scoped>
.management {max-width:960px;margin:24px auto;padding:0 20px;overflow-wrap:anywhere;}
.history-row {display:flex;justify-content:space-between;gap:12px;padding:8px 0;border-bottom:1px solid var(--td-component-border);}
details {margin-top:24px;}
summary {cursor:pointer;}
.item {padding:12px 0;border-bottom:1px solid var(--td-component-border);}
.warning {color:var(--td-warning-color);}
</style>
