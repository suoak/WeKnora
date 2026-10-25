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
        <p v-for="item in batch.items" :key="item.id">第 {{ item.ordinal + 1 }} 篇：{{ simpleStatus(item.status) }}</p>
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
import {feishuStatus,authorizeFeishu,listFeishuBatches,listMyFeishuBatches,getFeishuBatch,type FeishuStatus,type FeishuBatch,type FeishuBatchHistory} from '@/api/feishuPersonal'
import {FEISHU_PENDING} from '@/i18n/feishuPersonal'
import FeishuAccessManagement from './components/FeishuAccessManagement.vue'
const route=useRoute(), kb=typeof route.query.kb === 'string' ? route.query.kb : '', status=ref<FeishuStatus>()
const history=ref<FeishuBatchHistory[]>([]), batch=ref<FeishuBatch>(), more=ref(false), message=ref('')
let mounted=true
onUnmounted(()=>{mounted=false})
function simpleStatus(state:string) {
  if (['succeeded','existing','duplicate'].includes(state)) return '已完成'
  if (['failed','blocked','partial_failed','needs_attention'].includes(state)) return '需要处理'
  if (['processing','dispatched','running'].includes(state)) return '导入中'
  return '排队中'
}
onMounted(async()=>{
  try {
    status.value=await feishuStatus()
    await loadHistory(false)
    if(typeof route.query.batch==='string') await showBatch(route.query.batch)
  } catch {message.value='暂时无法读取导入记录，请稍后刷新。'}
})
async function loadHistory(append:boolean) {
  try {const offset=append?history.value.length:0; const rows=await (kb?listFeishuBatches(kb,offset):listMyFeishuBatches(offset)); if(!mounted)return; history.value=append?[...history.value,...rows]:rows; more.value=rows.length===20}
  catch {if(mounted)message.value='暂时无法读取导入记录，请稍后刷新。'}
}
async function showBatch(id:string) {
  try {const row=await getFeishuBatch(id); if(mounted && (!kb || row.batch.kb_id===kb))batch.value=row}
  catch {if(mounted)message.value='暂时无法读取任务，请稍后刷新。'}
}
async function connect(links:string) { sessionStorage.setItem(FEISHU_PENDING,JSON.stringify({kbId:kb,url:links,path:location.pathname+location.search})); const result=await authorizeFeishu(), target=new URL(result.authorization_url); if(target.origin==='https://accounts.feishu.cn') location.assign(target.href) }
</script>
<style scoped>
.management {max-width:960px;margin:24px auto;padding:0 20px;overflow-wrap:anywhere;}
.history-row {display:flex;justify-content:space-between;gap:12px;padding:8px 0;border-bottom:1px solid var(--td-component-border);}
details {margin-top:24px;}
summary {cursor:pointer;}
</style>
