<template>
  <section class="requests-panel">
    <div class="panel-head"><div><h3>访问申请</h3><p>仅空间 Owner 可审批。通过后固定授予 Viewer 只读权限。</p></div><t-button variant="outline" size="small" :loading="loading" @click="load">刷新</t-button></div>
    <div v-if="loading&&!requests.length" class="state"><t-loading text="加载访问申请…" /></div>
    <div v-else-if="!pending.length" class="state">当前没有待审核的 Portal 访问申请。</div>
    <div v-else class="request-list">
      <article v-for="request in pending" :key="request.id"><div class="request-main"><strong>申请人 {{ request.applicant_user_id }}</strong><span>{{ formatDate(request.created_at) }} · Viewer / 只读访问</span><p>{{ request.reason }}</p></div><div class="actions"><t-button size="small" theme="success" :loading="acting===request.id" @click="review(request,'approve')">批准</t-button><t-button size="small" theme="danger" variant="outline" :loading="acting===request.id" @click="review(request,'reject')">拒绝</t-button></div></article>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { approveTenantPortalAccessRequest,listTenantPortalAccessRequests,rejectTenantPortalAccessRequest,type TenantAccessRequest } from '@/api/portal'
const props=defineProps<{tenantId:number}>()
const requests=ref<TenantAccessRequest[]>([]),loading=ref(false),acting=ref('')
const pending=computed(()=>requests.value.filter(item=>item.status==='pending'))
async function load(){if(!props.tenantId)return;loading.value=true;try{const response=await listTenantPortalAccessRequests(props.tenantId);requests.value=response.data||[]}catch(error:any){MessagePlugin.error(error?.message||'访问申请加载失败')}finally{loading.value=false}}
async function review(request:TenantAccessRequest,action:'approve'|'reject'){acting.value=request.id;try{if(action==='approve')await approveTenantPortalAccessRequest(props.tenantId,request.id);else await rejectTenantPortalAccessRequest(props.tenantId,request.id);MessagePlugin.success(action==='approve'?'已授予 Viewer 权限':'已拒绝申请');await load()}catch(error:any){const status=Number(error?.$httpStatus||error?.status||0);const message=String(error?.message||'');if(action==='approve'&&status===409&&(message.includes('applicant')||message.includes('active user')))MessagePlugin.error('当前申请人账号不可用，无法授予空间权限。');else MessagePlugin.error(message||'处理访问申请失败')}finally{acting.value=''}}
const formatDate=(value:string)=>new Intl.DateTimeFormat('zh-CN',{dateStyle:'medium',timeStyle:'short'}).format(new Date(value))
watch(()=>props.tenantId,()=>void load())
onMounted(load)
</script>
<style scoped lang="less">
.requests-panel{margin-top:18px}.panel-head{display:flex;align-items:start;justify-content:space-between;margin-bottom:15px}.panel-head h3{margin:0;font-size:16px}.panel-head p{margin:5px 0 0;color:var(--td-text-color-secondary);font-size:13px}.state{min-height:130px;display:grid;place-items:center;border:1px dashed var(--td-component-border);border-radius:12px;color:var(--td-text-color-secondary)}.request-list{display:flex;flex-direction:column;gap:10px}.request-list article{display:flex;justify-content:space-between;gap:20px;padding:16px;border:1px solid var(--td-component-border);border-radius:12px;background:var(--td-bg-color-container)}.request-main span{display:block;color:var(--td-text-color-placeholder);font-size:12px;margin-top:4px}.request-main p{margin:10px 0 0;white-space:pre-wrap;color:var(--td-text-color-secondary)}.actions{display:flex;align-items:center;gap:8px}@media(max-width:640px){.request-list article{flex-direction:column}.actions{align-self:flex-end}}
</style>
