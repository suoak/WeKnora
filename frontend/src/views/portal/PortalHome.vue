<template>
  <div class="portal-page">
    <header class="portal-header">
      <router-link to="/portal" class="brand"><span class="brand-mark">K</span><span><strong>KnowHub 知汇</strong><small>CSBU IPD 知识门户</small></span></router-link>
      <div class="header-actions">
        <router-link v-if="authStore.isSystemAdmin" to="/portal/admin"><t-button variant="text">平台管理 · 知识门户</t-button></router-link>
        <UserMenu />
      </div>
    </header>

    <main>
      <section class="hero"><span class="eyebrow">KNOWLEDGE DISCOVERY</span><h1>发现组织中经过治理的知识空间</h1><p>沿 IPD 流程浏览专业知识，申请所需空间的 Viewer 只读权限。</p></section>
      <MySpacesSection :spaces="portal.mySpaces" @enter="enterMySpace" />
      <section class="discovery">
        <div class="section-title"><div><h2>IPD 知识导航</h2><p>阶段用于流程筛选，知识领域用于组织分类；同一空间只展示一次。</p></div><span>{{ portal.spaces.length }} 个空间</span></div>
        <IpdStageNavigator v-model="portal.selectedStage" :stages="portal.stages" />
        <PortalFilters v-model:search="portal.search" v-model:category="portal.selectedCategory" :categories="portal.categories" />
        <PortalSpaceGrid :spaces="portal.spaces" :loading="portal.loading" :empty-text="emptyText" :stage-labels="stageLabels"
          @request="openRequest" @enter="enterPortalSpace" @interaction="enterInteraction" />
      </section>
    </main>
    <AccessRequestDialog :visible="Boolean(requestSpace)" :space="requestSpace" :submitting="requestSubmitting" @close="requestSpace=null" @submit="submitRequest" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import UserMenu from '@/components/UserMenu.vue'
import MySpacesSection from '@/components/portal/MySpacesSection.vue'
import IpdStageNavigator from '@/components/portal/IpdStageNavigator.vue'
import PortalFilters from '@/components/portal/PortalFilters.vue'
import PortalSpaceGrid from '@/components/portal/PortalSpaceGrid.vue'
import AccessRequestDialog from '@/components/portal/AccessRequestDialog.vue'
import { usePortalStore } from '@/stores/portal'
import { useAuthStore } from '@/stores/auth'
import { resolvePortalInteraction, type PortalMySpace, type PortalSpace } from '@/api/portal'
import { switchWorkspaceAndNavigate } from '@/utils/tenantSwitch'

const portal=usePortalStore()
const authStore=useAuthStore()
const requestSpace=ref<PortalSpace|null>(null)
const requestSubmitting=computed(()=>requestSpace.value ? portal.requestState[requestSpace.value.tenant_id]==='submitting' : false)
let searchTimer:number|undefined

const stageLabels=computed(()=>Object.fromEntries(portal.stages.map(stage=>[stage.key,stage.name||stage.key])))
const emptyText=computed(()=>{
  if(portal.search.trim()||portal.selectedCategory) return '未找到匹配的知识空间。'
  if(portal.selectedStage!=='all') return '当前阶段暂无已发布知识空间。'
  return '当前暂无可发现的知识空间。'
})

watch(()=>portal.search,()=>{window.clearTimeout(searchTimer);searchTimer=window.setTimeout(()=>void portal.loadSpaces(),320)})
watch([()=>portal.selectedStage,()=>portal.selectedCategory],()=>void portal.loadSpaces())

function switchTo(tenantId:number,tenantName:string,role?:string){
  const labels:Record<string,string>={owner:'Owner',admin:'Admin',contributor:'Contributor',viewer:'Viewer'}
  switchWorkspaceAndNavigate({tenantId,tenantName,role,roleLabel:role?labels[role]:undefined})
}
const enterMySpace=(space:PortalMySpace)=>switchTo(space.tenant_id,space.tenant_name,space.role)
const enterPortalSpace=(space:PortalSpace)=>switchTo(space.tenant_id,space.display_name,space.current_role||undefined)
const openRequest=(space:PortalSpace)=>{requestSpace.value=space}

async function submitRequest(reason:string){
  if(!requestSpace.value)return
  try{await portal.requestAccess(requestSpace.value.tenant_id,reason);requestSpace.value=null;MessagePlugin.success('访问申请已提交，等待空间 Owner 审核。')}
  catch(error:any){MessagePlugin.error(error?.message||'提交访问申请失败')}
}

async function enterInteraction(space:PortalSpace){
  try{
    const response=await resolvePortalInteraction(space.tenant_id)
    if(response.data?.action==='navigate'&&response.data.path) window.location.assign(response.data.path)
  }catch(error:any){
    const status=Number(error?.$httpStatus||error?.status||0)
    if(status===403||status===409){MessagePlugin.warning('权限状态已经变化，请重新确认可访问范围。');await portal.loadSpaces();return}
    MessagePlugin.error(error?.message||'无法进入交互空间')
  }
}

onMounted(async()=>{try{await portal.initialize()}catch(error:any){MessagePlugin.error(error?.message||'知识门户加载失败')}})
</script>

<style scoped lang="less">
.portal-page{min-height:100vh;background:radial-gradient(circle at 15% 0,rgba(13,148,136,.12),transparent 32%),var(--td-bg-color-page);color:var(--td-text-color-primary)}.portal-header{height:68px;padding:0 clamp(20px,5vw,72px);display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid var(--td-component-stroke);background:color-mix(in srgb,var(--td-bg-color-container) 92%,transparent);position:sticky;top:0;z-index:10;backdrop-filter:blur(16px)}.brand{display:flex;align-items:center;gap:11px;color:inherit;text-decoration:none}.brand-mark{width:38px;height:38px;display:grid;place-items:center;border-radius:11px;color:#fff;font-weight:800;background:linear-gradient(135deg,#0f766e,#2563eb)}.brand strong,.brand small{display:block}.brand small{color:var(--td-text-color-secondary);margin-top:2px}.header-actions{display:flex;align-items:center;gap:8px}main{max-width:1280px;margin:auto;padding:0 clamp(20px,5vw,56px) 70px}.hero{padding:70px 0 42px;max-width:760px}.eyebrow{font-size:12px;letter-spacing:.18em;color:var(--td-brand-color);font-weight:700}.hero h1{font-size:clamp(34px,5vw,56px);line-height:1.08;margin:12px 0 18px;letter-spacing:-.035em}.hero p,.section-title p{color:var(--td-text-color-secondary);line-height:1.7}.discovery{margin-top:52px;display:flex;flex-direction:column;gap:20px}.section-title{display:flex;align-items:end;justify-content:space-between}.section-title h2{margin:0;font-size:24px}.section-title p{margin:6px 0 0}.section-title>span{color:var(--td-text-color-placeholder);font-size:13px}@media(max-width:680px){.portal-header{padding:0 14px}.brand small{display:none}.hero{padding-top:44px}.header-actions>a{display:none}.section-title{align-items:start;flex-direction:column;gap:8px}}
</style>
