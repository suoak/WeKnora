<template>
  <section class="requests-panel">
    <div class="panel-head"><div><h3>{{ t('portal.requests.title') }}</h3><p>{{ t('portal.requests.description') }}</p></div><t-button variant="outline" size="small" :loading="loading" @click="load">{{ t('portal.admin.refresh') }}</t-button></div>
    <div v-if="loading&&!requests.length" class="state"><t-loading :text="t('portal.requests.loading')" /></div>
    <div v-else-if="!pending.length" class="state">{{ t('portal.requests.empty') }}</div>
    <div v-else class="request-list">
      <article v-for="request in pending" :key="request.id"><div class="request-main"><strong>{{ t('portal.requests.applicant', { id: request.applicant_user_id }) }}</strong><span>{{ formatDate(request.created_at) }} · {{ t('portal.viewerOnly') }}</span><p>{{ request.reason }}</p></div><div class="actions"><t-button size="small" theme="success" :loading="acting===request.id" @click="review(request,'approve')">{{ t('portal.requests.approve') }}</t-button><t-button size="small" theme="danger" variant="outline" :loading="acting===request.id" @click="review(request,'reject')">{{ t('portal.requests.reject') }}</t-button></div></article>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { approveTenantPortalAccessRequest,listTenantPortalAccessRequests,rejectTenantPortalAccessRequest,type TenantAccessRequest } from '@/api/portal'
const props=defineProps<{tenantId:number}>()
const { t, locale }=useI18n()
const requests=ref<TenantAccessRequest[]>([]),loading=ref(false),acting=ref('')
const pending=computed(()=>requests.value.filter(item=>item.status==='pending'))
async function load(){if(!props.tenantId)return;loading.value=true;try{const response=await listTenantPortalAccessRequests(props.tenantId);requests.value=response.data||[]}catch(error:any){MessagePlugin.error(error?.message||t('portal.requests.loadFailed'))}finally{loading.value=false}}
async function review(request:TenantAccessRequest,action:'approve'|'reject'){acting.value=request.id;try{if(action==='approve')await approveTenantPortalAccessRequest(props.tenantId,request.id);else await rejectTenantPortalAccessRequest(props.tenantId,request.id);MessagePlugin.success(t(action==='approve'?'portal.requests.approveSuccess':'portal.requests.rejectSuccess'));await load()}catch(error:any){const status=Number(error?.$httpStatus||error?.status||0);const message=String(error?.message||'');if(action==='approve'&&status===409&&(message.includes('applicant')||message.includes('active user')))MessagePlugin.error(t('portal.requests.applicantUnavailable'));else MessagePlugin.error(message||t('portal.requests.reviewFailed'))}finally{acting.value=''}}
const formatDate=(value:string)=>new Intl.DateTimeFormat(locale.value,{dateStyle:'medium',timeStyle:'short'}).format(new Date(value))
watch(()=>props.tenantId,()=>void load())
onMounted(load)
</script>
<style scoped lang="less">
.requests-panel{margin-top:18px}.panel-head{display:flex;align-items:start;justify-content:space-between;margin-bottom:15px}.panel-head h3{margin:0;font-size:16px}.panel-head p{margin:5px 0 0;color:var(--td-text-color-secondary);font-size:13px}.state{min-height:130px;display:grid;place-items:center;border:1px dashed var(--td-component-border);border-radius:12px;color:var(--td-text-color-secondary)}.request-list{display:flex;flex-direction:column;gap:10px}.request-list article{display:flex;justify-content:space-between;gap:20px;padding:16px;border:1px solid var(--td-component-border);border-radius:12px;background:var(--td-bg-color-container)}.request-main span{display:block;color:var(--td-text-color-placeholder);font-size:12px;margin-top:4px}.request-main p{margin:10px 0 0;white-space:pre-wrap;color:var(--td-text-color-secondary)}.actions{display:flex;align-items:center;gap:8px}@media(max-width:640px){.request-list article{flex-direction:column}.actions{align-self:flex-end}}
</style>
