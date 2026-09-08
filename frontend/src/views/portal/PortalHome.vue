<template>
  <div class="portal-page">
    <header class="portal-header">
      <router-link to="/portal" class="brand"><span class="brand-mark">K</span><span><strong>KnowHub 知汇</strong><small>{{ t('portal.brandSubtitle') }}</small></span></router-link>
      <div class="header-actions">
        <router-link v-if="authStore.isSystemAdmin" to="/portal/admin"><t-button variant="text">{{ t('portal.admin.menuEntry') }}</t-button></router-link>
        <UserMenu />
      </div>
    </header>

    <main>
      <section class="hero"><span class="eyebrow">{{ t('portal.heroEyebrow') }}</span><h1>{{ t('portal.heroTitle') }}</h1><p>{{ t('portal.heroDescription') }}</p></section>
      <MySpacesSection :spaces="portal.mySpaces" @enter="enterMySpace" />
      <section class="discovery">
        <div class="portal-tabs" role="tablist" :aria-label="t('portal.viewSwitcherLabel')">
          <button :class="{ active: viewMode==='ipd' }" @click="selectView('ipd')"><t-icon name="git-branch" /><span><strong>{{ t('portal.views.ipd') }}</strong><small>{{ t('portal.views.ipdDescription') }}</small></span></button>
          <button :class="{ active: viewMode==='public' }" @click="selectView('public')"><t-icon name="browse" /><span><strong>{{ t('portal.views.public') }}</strong><small>{{ t('portal.views.publicDescription') }}</small></span></button>
        </div>
        <template v-if="viewMode==='ipd'">
          <div class="section-title"><div><h2>{{ t('portal.navigationTitle') }}</h2><p>{{ t('portal.navigationDescription') }}</p></div><span>{{ t('portal.spaceCount', { count: portal.spaces.length }) }}</span></div>
          <IpdFlowOverview :stages="portal.stages" :spaces="portal.overviewSpaces" :selected-stage="portal.selectedStage" @select="selectStage" />
          <IpdStageDetail :selected-stage="portal.selectedStage" :stages="portal.stages" :space-count="portal.spaces.length" />
        </template>
        <section v-else class="public-overview">
          <div class="public-mark"><t-icon name="browse" /></div>
          <div><span>{{ t('portal.publicZone.eyebrow') }}</span><h2>{{ t('portal.publicZone.title') }}</h2><p>{{ t('portal.publicZone.description') }}</p></div>
          <div class="public-types"><span v-for="item in publicTypes" :key="item">{{ item }}</span></div>
        </section>
        <PortalFilters v-model:search="portal.search" v-model:category="portal.selectedCategory" :categories="portal.categories" :show-category="viewMode==='ipd'" />
        <PortalSpaceGrid :spaces="portal.spaces" :loading="portal.loading" :empty-text="emptyText" :stage-labels="stageLabels"
          @request="openRequest" @enter="enterPortalSpace" @interaction="enterInteraction" />
      </section>
    </main>
    <AccessRequestDialog :visible="Boolean(requestSpace)" :space="requestSpace" :submitting="requestSubmitting" @close="requestSpace=null" @submit="submitRequest" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import UserMenu from '@/components/UserMenu.vue'
import MySpacesSection from '@/components/portal/MySpacesSection.vue'
import IpdFlowOverview from '@/components/portal/IpdFlowOverview.vue'
import IpdStageDetail from '@/components/portal/IpdStageDetail.vue'
import PortalFilters from '@/components/portal/PortalFilters.vue'
import PortalSpaceGrid from '@/components/portal/PortalSpaceGrid.vue'
import AccessRequestDialog from '@/components/portal/AccessRequestDialog.vue'
import { usePortalStore } from '@/stores/portal'
import { useAuthStore } from '@/stores/auth'
import { resolvePortalInteraction, type PortalMySpace, type PortalSpace } from '@/api/portal'
import { switchWorkspaceAndNavigate } from '@/utils/tenantSwitch'

const portal=usePortalStore()
const authStore=useAuthStore()
const { t }=useI18n()
const PUBLIC_CATEGORY='public_knowledge'
const viewMode=ref<'ipd'|'public'>(portal.selectedCategory===PUBLIC_CATEGORY?'public':'ipd')
const requestSpace=ref<PortalSpace|null>(null)
const requestSubmitting=computed(()=>requestSpace.value ? portal.requestState[requestSpace.value.tenant_id]==='submitting' : false)
let searchTimer:number|undefined

const stageLabels=computed(()=>Object.fromEntries(portal.stages.map(stage=>[stage.key,t(`portal.stages.${stage.key}.name`)])))
const emptyText=computed(()=>{
  if(viewMode.value==='public'&&!portal.search.trim()) return t('portal.publicZone.empty')
  if(portal.search.trim()||portal.selectedCategory) return t('portal.emptySearch')
  if(portal.selectedStage!=='all') return t('portal.emptyStage')
  return t('portal.emptyAll')
})
const publicTypes=computed(()=>['standards','templates','training','practices'].map(key=>t(`portal.publicZone.types.${key}`)))

watch(()=>portal.search,()=>{window.clearTimeout(searchTimer);searchTimer=window.setTimeout(()=>void portal.loadSpaces(),320)})
watch([()=>portal.selectedStage,()=>portal.selectedCategory],()=>void portal.loadSpaces())

function selectView(mode:'ipd'|'public'){
  viewMode.value=mode
  portal.selectedStage='all'
  portal.selectedCategory=mode==='public'?PUBLIC_CATEGORY:''
}
function selectStage(stage:string){portal.selectedStage=portal.selectedStage===stage?'all':stage}

function switchTo(tenantId:number,tenantName:string,role?:string){
  switchWorkspaceAndNavigate({tenantId,tenantName,role,roleLabel:role?t(`portal.roles.${role}`):undefined})
}
const enterMySpace=(space:PortalMySpace)=>switchTo(space.tenant_id,space.tenant_name,space.role)
const enterPortalSpace=(space:PortalSpace)=>switchTo(space.tenant_id,space.display_name,space.current_role||undefined)
const openRequest=(space:PortalSpace)=>{requestSpace.value=space}

async function submitRequest(reason:string){
  if(!requestSpace.value)return
  try{await portal.requestAccess(requestSpace.value.tenant_id,reason);requestSpace.value=null;MessagePlugin.success(t('portal.requestSuccess'))}
  catch(error:any){MessagePlugin.error(error?.message||t('portal.requestFailed'))}
}

async function enterInteraction(space:PortalSpace){
  try{
    const response=await resolvePortalInteraction(space.tenant_id)
    if(response.data?.action==='navigate'&&response.data.path) window.location.assign(response.data.path)
  }catch(error:any){
    const status=Number(error?.$httpStatus||error?.status||0)
    if(status===403||status===409){MessagePlugin.warning(t('portal.permissionChanged'));await portal.loadSpaces();return}
    MessagePlugin.error(error?.message||t('portal.interactionFailed'))
  }
}

onMounted(async()=>{try{await portal.initialize()}catch(error:any){MessagePlugin.error(error?.message||t('portal.loadFailed'))}})
</script>

<style scoped lang="less">
.portal-page{min-height:100vh;background:radial-gradient(circle at 15% 0,rgba(13,148,136,.12),transparent 32%),var(--td-bg-color-page);color:var(--td-text-color-primary)}.portal-header{height:68px;padding:0 clamp(20px,5vw,72px);display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid var(--td-component-stroke);background:color-mix(in srgb,var(--td-bg-color-container) 92%,transparent);position:sticky;top:0;z-index:10;backdrop-filter:blur(16px)}.brand{display:flex;align-items:center;gap:11px;color:inherit;text-decoration:none}.brand-mark{width:38px;height:38px;display:grid;place-items:center;border-radius:11px;color:#fff;font-weight:800;background:linear-gradient(135deg,#0f766e,#2563eb)}.brand strong,.brand small{display:block}.brand small{color:var(--td-text-color-secondary);margin-top:2px}.header-actions{display:flex;align-items:center;gap:8px}main{max-width:1380px;margin:auto;padding:0 clamp(20px,5vw,56px) 70px}.hero{padding:52px 0 34px;max-width:820px}.eyebrow{font-size:12px;letter-spacing:.18em;color:var(--td-brand-color);font-weight:700}.hero h1{font-size:clamp(34px,5vw,54px);line-height:1.08;margin:12px 0 18px;letter-spacing:-.035em}.hero p,.section-title p{color:var(--td-text-color-secondary);line-height:1.7}.discovery{margin-top:38px;display:flex;flex-direction:column;gap:20px}.portal-tabs{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.portal-tabs button{display:flex;align-items:center;gap:13px;padding:15px 18px;border:1px solid var(--td-component-border);border-radius:14px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer;text-align:left}.portal-tabs button>.t-icon{font-size:24px;color:var(--td-text-color-placeholder)}.portal-tabs button span,.portal-tabs button strong,.portal-tabs button small{display:block}.portal-tabs button small{margin-top:3px;color:var(--td-text-color-secondary)}.portal-tabs button.active{border-color:var(--td-brand-color);box-shadow:inset 3px 0 var(--td-brand-color)}.portal-tabs button.active>.t-icon{color:var(--td-brand-color)}.section-title{display:flex;align-items:end;justify-content:space-between}.section-title h2{margin:0;font-size:24px}.section-title p{margin:6px 0 0}.section-title>span{color:var(--td-text-color-placeholder);font-size:13px}.public-overview{display:grid;grid-template-columns:auto minmax(280px,1fr) auto;align-items:center;gap:18px;padding:23px;border:1px solid color-mix(in srgb,var(--td-brand-color) 28%,var(--td-component-border));border-radius:16px;background:linear-gradient(135deg,var(--td-brand-color-light),var(--td-bg-color-container) 60%)}.public-mark{width:52px;height:52px;display:grid;place-items:center;border-radius:15px;background:var(--td-brand-color);color:#fff;font-size:25px}.public-overview>div>span{color:var(--td-brand-color);font-size:12px;font-weight:700}.public-overview h2{margin:5px 0 6px}.public-overview p{margin:0;color:var(--td-text-color-secondary);line-height:1.6}.public-types{display:flex;justify-content:flex-end;flex-wrap:wrap;gap:7px}.public-types span{padding:6px 9px;border-radius:999px;background:var(--td-bg-color-container);border:1px solid var(--td-component-stroke);color:var(--td-text-color-secondary);font-size:12px}@media(max-width:800px){.public-overview{grid-template-columns:auto 1fr}.public-types{grid-column:1/-1;justify-content:flex-start}}@media(max-width:680px){.portal-header{padding:0 14px}.brand small{display:none}.hero{padding-top:38px}.header-actions>a{display:none}.portal-tabs{grid-template-columns:1fr}.section-title{align-items:start;flex-direction:column;gap:8px}}
</style>
