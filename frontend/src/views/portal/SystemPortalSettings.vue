<template>
  <div class="admin-page">
    <header><router-link to="/portal" class="back"><t-icon name="chevron-left" />{{ t('portal.admin.back') }}</router-link><div><strong>KnowHub 知汇</strong><small>{{ t('portal.admin.menuEntry') }}</small></div><UserMenu /></header>
    <main><div class="title"><div><span>{{ t('portal.admin.eyebrow') }}</span><h1>{{ t('portal.admin.title') }}</h1><p>{{ t('portal.admin.description') }}</p></div><t-button variant="outline" :loading="loading" @click="load">{{ t('portal.admin.refresh') }}</t-button></div>
      <div class="layout">
        <aside><button v-for="space in spaces" :key="space.tenant_id" :class="{active:space.tenant_id===selectedId}" @click="selectedId=space.tenant_id"><span><strong>{{ space.tenant_name }}</strong><small>{{ space.display_name||t('portal.admin.unconfigured') }}</small></span><t-tag size="small" :theme="space.status==='published'?'success':'default'">{{ t(`portal.admin.statuses.${space.status}`) }}</t-tag></button></aside>
        <PortalConfigEditor :config="selected" :stages="stages" :organizations="organizations" :saving="saving" :transitioning="transitioning" @save="save" @status="setStatus" />
      </div>
    </main>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import UserMenu from '@/components/UserMenu.vue'
import PortalConfigEditor from '@/components/portal/PortalConfigEditor.vue'
import { listAdminPortalSpaces,listPortalOrganizationOptions,listPortalStages,transitionAdminPortalStatus,updateAdminPortalConfig,type PortalAdminSpace,type PortalConfigPayload,type PortalOrganizationOption,type PortalStage } from '@/api/portal'
const spaces=ref<PortalAdminSpace[]>([]),stages=ref<PortalStage[]>([]),organizations=ref<PortalOrganizationOption[]>([])
const { t }=useI18n()
const selectedId=ref<number|null>(null),loading=ref(false),saving=ref(false),transitioning=ref(false)
const selected=computed(()=>spaces.value.find(item=>item.tenant_id===selectedId.value)||null)
async function load(){loading.value=true;try{const [spaceRes,stageRes,orgRes]=await Promise.all([listAdminPortalSpaces(),listPortalStages(),listPortalOrganizationOptions()]);spaces.value=spaceRes.data||[];stages.value=stageRes.data||[];organizations.value=orgRes.data||[];if(!spaces.value.some(item=>item.tenant_id===selectedId.value))selectedId.value=spaces.value[0]?.tenant_id||null}catch(error:any){MessagePlugin.error(error?.message||t('portal.admin.loadFailed'))}finally{loading.value=false}}
async function save(payload:PortalConfigPayload){if(!selectedId.value)return;saving.value=true;try{const response=await updateAdminPortalConfig(selectedId.value,payload);replace(response.data);MessagePlugin.success(t('portal.admin.saveSuccess'))}catch(error:any){MessagePlugin.error(error?.message||t('portal.admin.saveFailed'))}finally{saving.value=false}}
async function setStatus(action:'publish'|'unpublish'|'archive'){if(!selectedId.value)return;transitioning.value=true;try{const response=await transitionAdminPortalStatus(selectedId.value,action);replace(response.data);MessagePlugin.success(t('portal.admin.statusSuccess'))}catch(error:any){MessagePlugin.error(error?.message||t('portal.admin.statusFailed'))}finally{transitioning.value=false}}
function replace(item:PortalAdminSpace){const index=spaces.value.findIndex(space=>space.tenant_id===item.tenant_id);if(index>=0)spaces.value.splice(index,1,item)}
onMounted(load)
</script>
<style scoped lang="less">
.admin-page{min-height:100vh;background:var(--td-bg-color-page);color:var(--td-text-color-primary)}header{height:68px;padding:0 clamp(20px,5vw,70px);display:grid;grid-template-columns:1fr auto 1fr;align-items:center;border-bottom:1px solid var(--td-component-stroke);background:var(--td-bg-color-container)}header>div{text-align:center}header strong,header small{display:block}header small{color:var(--td-text-color-secondary);margin-top:2px}header>:last-child{justify-self:end}.back{display:flex;align-items:center;color:var(--td-text-color-secondary);text-decoration:none}main{max-width:1280px;margin:auto;padding:46px clamp(20px,4vw,52px)}.title{display:flex;justify-content:space-between;align-items:end;margin-bottom:28px}.title span{font-size:11px;letter-spacing:.16em;color:var(--td-brand-color);font-weight:700}.title h1{font-size:34px;margin:8px 0}.title p{margin:0;color:var(--td-text-color-secondary)}.layout{display:grid;grid-template-columns:280px 1fr;gap:18px}aside{display:flex;flex-direction:column;gap:7px}aside button{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:12px;border:1px solid var(--td-component-border);border-radius:11px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);text-align:left;cursor:pointer}aside button.active{border-color:var(--td-brand-color);box-shadow:inset 3px 0 var(--td-brand-color)}aside strong,aside small{display:block;max-width:155px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}aside small{color:var(--td-text-color-secondary);margin-top:3px}@media(max-width:850px){header{grid-template-columns:1fr auto}header>div{display:none}.layout{grid-template-columns:1fr}aside{display:grid;grid-template-columns:repeat(auto-fill,minmax(220px,1fr))}}
</style>
