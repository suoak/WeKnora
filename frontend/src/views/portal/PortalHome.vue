<template>
  <div class="portal-shell">
    <Menu />
    <div class="portal-page">
    <main>
      <section class="hero" data-guide="portal-home">
        <div class="hero-copy"><span class="eyebrow">KnowHub / {{ t('portalHome.brandZh') }}</span><h1>{{ t('portalHome.title') }}</h1><p>{{ t('portalHome.description') }}</p></div>
        <router-link v-if="authStore.isSystemAdmin" to="/portal/admin"><t-button variant="outline">{{ t('portal.admin.menuEntry') }}</t-button></router-link>
        <form class="quick-ask" data-guide="portal-quick-ask" @submit.prevent="startAsk">
          <t-input v-model="quickQuestion" size="large" :placeholder="t('portalHome.askPlaceholder')" clearable />
          <t-button type="submit" size="large" :disabled="!quickQuestion.trim()">{{ t('portalHome.askAction') }}</t-button>
        </form>
      </section>

      <section class="getting-started">
        <div class="section-title"><div><h2>{{ t('portalHome.useTitle') }}</h2><p>{{ t('portalHome.useDescription') }}</p></div></div>
        <div class="task-grid">
          <button v-for="task in usageTasks" :key="task.id" type="button" class="task-card" :data-guide="`portal-task-${task.id}`" @click="openTask(task.id)">
            <t-icon :name="task.icon" /><span><strong>{{ t(task.titleKey) }}</strong><small>{{ t(task.descriptionKey) }}</small></span><t-icon name="chevron-right" />
          </button>
        </div>
      </section>

      <section class="resource-overview">
        <div class="resource-column">
          <div class="section-title"><div><h2>{{ t('portalHome.knowledgeTitle') }}</h2><p>{{ t('portalHome.knowledgeDescription') }}</p></div><router-link to="/platform/knowledge-bases">{{ t('portalHome.viewAll') }}</router-link></div>
          <t-skeleton v-if="resourcesLoading" animation="gradient" :row-col="[1, 1, 1]" />
          <div v-else-if="knowledgeBases.length" class="compact-list"><button v-for="kb in knowledgeBases" :key="kb.id" @click="router.push(`/platform/knowledge-bases/${kb.id}`)"><t-icon name="folder" /><span><strong>{{ kb.name }}</strong><small>{{ kb.description || t('portalHome.noDescription') }}</small></span></button></div>
          <t-empty v-else :description="t('portalHome.noKnowledge')"><t-button variant="outline" @click="router.push('/platform/knowledge-bases')">{{ t('portalHome.browseKnowledge') }}</t-button></t-empty>
        </div>
        <div v-if="capabilities.isSupported('agents')" class="resource-column" data-guide="portal-agents">
          <div class="section-title"><div><h2>{{ t('portalHome.agentTitle') }}</h2><p>{{ t('portalHome.agentDescription') }}</p></div><router-link to="/platform/agents">{{ t('portalHome.viewAll') }}</router-link></div>
          <t-skeleton v-if="resourcesLoading" animation="gradient" :row-col="[1, 1, 1]" />
          <div v-else-if="agents.length" class="compact-list"><button v-for="agent in agents" :key="agent.id" @click="useAgent(agent.id)"><t-icon name="robot" /><span><strong>{{ agent.name }}</strong><small>{{ agent.description || t('portalHome.noDescription') }}</small></span><em>{{ t('portalHome.useAgent') }}</em></button></div>
          <t-empty v-else :description="t('portalHome.noAgents')"><t-button variant="outline" @click="router.push('/platform/agents')">{{ t('portalHome.exploreAgents') }}</t-button></t-empty>
        </div>
      </section>
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
        <PortalSpaceGrid :spaces="visibleSpaces" :loading="portal.loading" :empty-text="emptyText" :stage-labels="stageLabels"
          @request="openRequest" @enter="enterPortalSpace" @interaction="enterInteraction" />
        <div v-if="portal.spaces.length>PAGE_SIZE" class="result-pagination">
          <span>{{ t('portal.resultsShowing',{visible:visibleSpaces.length,total:portal.spaces.length}) }}</span>
          <t-button v-if="hasMoreSpaces" variant="outline" @click="visibleLimit+=PAGE_SIZE">{{ t('portal.loadMoreSpaces') }}</t-button>
          <t-button v-else variant="text" @click="visibleLimit=PAGE_SIZE">{{ t('portal.collapseSpaces') }}</t-button>
        </div>
      </section>
    </main>
    <AccessRequestDialog :visible="Boolean(requestSpace)" :space="requestSpace" :submitting="requestSubmitting" @close="requestSpace=null" @submit="submitRequest" />
    </div>
    <NewUserGuide />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import Menu from '@/components/menu.vue'
import NewUserGuide from '@/components/NewUserGuide.vue'
import MySpacesSection from '@/components/portal/MySpacesSection.vue'
import IpdFlowOverview from '@/components/portal/IpdFlowOverview.vue'
import IpdStageDetail from '@/components/portal/IpdStageDetail.vue'
import PortalFilters from '@/components/portal/PortalFilters.vue'
import PortalSpaceGrid from '@/components/portal/PortalSpaceGrid.vue'
import AccessRequestDialog from '@/components/portal/AccessRequestDialog.vue'
import { usePortalStore } from '@/stores/portal'
import { useAuthStore } from '@/stores/auth'
import { useMenuStore } from '@/stores/menu'
import { useSettingsStore } from '@/stores/settings'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { listKnowledgeBases } from '@/api/knowledge-base'
import { listAgents, type CustomAgent } from '@/api/agent'
import type { KnowledgeBaseInfo } from '@/api/auth'
import { resolvePortalInteraction, type PortalMySpace, type PortalSpace } from '@/api/portal'
import { switchWorkspaceAndNavigate } from '@/utils/tenantSwitch'

const portal=usePortalStore()
const authStore=useAuthStore()
const menuStore=useMenuStore()
const settingsStore=useSettingsStore()
const capabilities=useDeploymentCapabilitiesStore()
const router=useRouter()
const { t }=useI18n()
const PUBLIC_CATEGORY='public_knowledge'
const PAGE_SIZE=12
const viewMode=ref<'ipd'|'public'>(portal.selectedCategory===PUBLIC_CATEGORY?'public':'ipd')
const visibleLimit=ref(PAGE_SIZE)
const requestSpace=ref<PortalSpace|null>(null)
const quickQuestion=ref('')
const knowledgeBases=ref<KnowledgeBaseInfo[]>([])
const agents=ref<CustomAgent[]>([])
const resourcesLoading=ref(true)
const requestSubmitting=computed(()=>requestSpace.value ? portal.requestState[requestSpace.value.tenant_id]==='submitting' : false)
let searchTimer:number|undefined

const usageTasks = computed(() => [
  { id: 'ask', icon: 'chat', titleKey: 'portalHome.tasks.ask.title', descriptionKey: 'portalHome.tasks.ask.description' },
  { id: 'knowledge', icon: 'folder', titleKey: 'portalHome.tasks.knowledge.title', descriptionKey: 'portalHome.tasks.knowledge.description' },
  ...(capabilities.isSupported('agents') ? [{ id: 'agent', icon: 'robot', titleKey: 'portalHome.tasks.agent.title', descriptionKey: 'portalHome.tasks.agent.description' }] : []),
  ...(capabilities.isSupported('settings.mcp') ? [{ id: 'mcp', icon: 'connection', titleKey: 'portalHome.tasks.mcp.title', descriptionKey: 'portalHome.tasks.mcp.description' }] : []),
])

function startAsk(){
  const question=quickQuestion.value.trim()
  if(!question)return
  menuStore.setPrefillQuery(question)
  void router.push('/platform/creatChat')
}
function openTask(id:string){
  if(id==='ask'){void router.push('/platform/creatChat');return}
  if(id==='knowledge'){document.querySelector('.discovery')?.scrollIntoView({behavior:'smooth'});return}
  if(id==='agent'){void router.push('/platform/agents');return}
  void router.push('/platform/settings?section=mcp-access-keys')
}
function useAgent(id:string){settingsStore.selectAgent(id);void router.push('/platform/creatChat')}

async function loadResourceOverview(){
  resourcesLoading.value=true
  try{
    const [kbResponse,agentResponse]=await Promise.all([
      listKnowledgeBases(),
      capabilities.isSupported('agents')?listAgents():Promise.resolve({data:[]}),
    ]) as any[]
    knowledgeBases.value=(kbResponse?.data||[]).slice(0,4)
    agents.value=(agentResponse?.data||[]).slice(0,4)
  }catch{knowledgeBases.value=[];agents.value=[]}
  finally{resourcesLoading.value=false}
}

const stageLabels=computed(()=>Object.fromEntries(portal.stages.map(stage=>[stage.key,t(`portal.stages.${stage.key}.name`)])))
const emptyText=computed(()=>{
  if(viewMode.value==='public'&&!portal.search.trim()) return t('portal.publicZone.empty')
  if(portal.search.trim()||portal.selectedCategory) return t('portal.emptySearch')
  if(portal.selectedStage!=='all') return t('portal.emptyStage')
  return t('portal.emptyAll')
})
const publicTypes=computed(()=>['standards','templates','training','practices'].map(key=>t(`portal.publicZone.types.${key}`)))
const visibleSpaces=computed(()=>portal.spaces.slice(0,visibleLimit.value))
const hasMoreSpaces=computed(()=>visibleSpaces.value.length<portal.spaces.length)

watch(()=>portal.search,()=>{visibleLimit.value=PAGE_SIZE;window.clearTimeout(searchTimer);searchTimer=window.setTimeout(()=>void portal.loadSpaces(),320)})
watch([()=>portal.selectedStage,()=>portal.selectedCategory],()=>{visibleLimit.value=PAGE_SIZE;void portal.loadSpaces()})

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

onMounted(async()=>{void loadResourceOverview();try{await portal.initialize()}catch(error:any){MessagePlugin.error(error?.message||t('portal.loadFailed'))}})
</script>

<style scoped lang="less">
.portal-shell{width:100%;height:100%;min-height:100vh;display:flex;overflow:hidden;background:var(--td-bg-color-page)}.portal-page{flex:1;min-width:0;min-height:0;overflow-y:auto;background:radial-gradient(circle at 15% 0,rgba(13,148,136,.12),transparent 32%),var(--td-bg-color-page);color:var(--td-text-color-primary)}main{max-width:1380px;margin:auto;padding:0 clamp(20px,5vw,56px) 70px}.hero{padding:52px 0 34px;display:flex;align-items:flex-start;justify-content:space-between;gap:24px}.hero>div{max-width:820px}.eyebrow{font-size:12px;letter-spacing:.18em;color:var(--td-brand-color);font-weight:700}.hero h1{font-size:clamp(34px,5vw,54px);line-height:1.08;margin:12px 0 18px;letter-spacing:-.035em}.hero p,.section-title p{color:var(--td-text-color-secondary);line-height:1.7}.discovery{margin-top:38px;display:flex;flex-direction:column;gap:20px}.portal-tabs{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.portal-tabs button{display:flex;align-items:center;gap:13px;padding:15px 18px;border:1px solid var(--td-component-border);border-radius:14px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer;text-align:left}.portal-tabs button>.t-icon{font-size:24px;color:var(--td-text-color-placeholder)}.portal-tabs button span,.portal-tabs button strong,.portal-tabs button small{display:block}.portal-tabs button small{margin-top:3px;color:var(--td-text-color-secondary)}.portal-tabs button.active{border-color:var(--td-brand-color);box-shadow:inset 3px 0 var(--td-brand-color)}.portal-tabs button.active>.t-icon{color:var(--td-brand-color)}.section-title{display:flex;align-items:end;justify-content:space-between}.section-title h2{margin:0;font-size:24px}.section-title p{margin:6px 0 0}.section-title>span{color:var(--td-text-color-placeholder);font-size:13px}.public-overview{display:grid;grid-template-columns:auto minmax(280px,1fr) auto;align-items:center;gap:18px;padding:23px;border:1px solid color-mix(in srgb,var(--td-brand-color) 28%,var(--td-component-border));border-radius:16px;background:linear-gradient(135deg,var(--td-brand-color-light),var(--td-bg-color-container) 60%)}.public-mark{width:52px;height:52px;display:grid;place-items:center;border-radius:15px;background:var(--td-brand-color);color:#fff;font-size:25px}.public-overview>div>span{color:var(--td-brand-color);font-size:12px;font-weight:700}.public-overview h2{margin:5px 0 6px}.public-overview p{margin:0;color:var(--td-text-color-secondary);line-height:1.6}.public-types{display:flex;justify-content:flex-end;flex-wrap:wrap;gap:7px}.public-types span{padding:6px 9px;border-radius:999px;background:var(--td-bg-color-container);border:1px solid var(--td-component-stroke);color:var(--td-text-color-secondary);font-size:12px}.result-pagination{display:flex;align-items:center;justify-content:center;gap:14px;padding:8px;color:var(--td-text-color-secondary);font-size:13px}@media(max-width:800px){.public-overview{grid-template-columns:auto 1fr}.public-types{grid-column:1/-1;justify-content:flex-start}}@media(max-width:680px){.hero{padding-top:38px;flex-direction:column}.portal-tabs{grid-template-columns:1fr}.section-title{align-items:start;flex-direction:column;gap:8px}}
.portal-page{background:var(--td-bg-color-page)}main{padding:0 clamp(18px,4vw,52px) 70px}.hero{margin-top:24px;padding:25px 28px;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:14px 24px;border:1px solid var(--td-component-stroke);border-radius:16px;background:var(--td-bg-color-container)}.hero>.hero-copy{max-width:760px}.hero h1{font-size:clamp(28px,4vw,40px);margin:8px 0 10px}.quick-ask{grid-column:1/-1;display:flex;gap:10px;max-width:860px}.quick-ask :deep(.t-input){height:42px}.getting-started{margin-top:30px}.task-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin-top:14px}.task-card{display:flex;align-items:center;gap:11px;min-width:0;padding:15px;border:1px solid var(--td-component-stroke);border-radius:10px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer;text-align:left}.task-card:hover{border-color:var(--td-brand-color)}.task-card>.t-icon:first-child{font-size:22px;color:var(--td-brand-color)}.task-card>.t-icon:last-child{margin-left:auto;color:var(--td-text-color-placeholder)}.task-card span{display:flex;flex:1;min-width:0;flex-direction:column}.task-card small{margin-top:4px;color:var(--td-text-color-secondary);line-height:1.4}.resource-overview{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px;margin-top:30px}.resource-column{min-width:0;padding:20px;border:1px solid var(--td-component-stroke);border-radius:12px;background:var(--td-bg-color-container)}.resource-column .section-title{margin-bottom:12px}.section-title a{color:var(--td-brand-color);font-size:13px}.compact-list{display:grid;gap:5px}.compact-list button{display:flex;align-items:center;gap:10px;min-width:0;padding:10px;border:0;border-radius:7px;background:transparent;color:var(--td-text-color-primary);cursor:pointer;text-align:left}.compact-list button:hover{background:var(--td-bg-color-container-hover)}.compact-list button>.t-icon{font-size:19px;color:var(--td-brand-color)}.compact-list button span{display:flex;flex:1;min-width:0;flex-direction:column}.compact-list strong,.compact-list small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.compact-list small{margin-top:2px;color:var(--td-text-color-secondary)}.compact-list em{color:var(--td-brand-color);font-size:12px;font-style:normal}@media(max-width:1100px){.task-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:800px){main{padding-left:58px}.resource-overview{grid-template-columns:1fr}}@media(max-width:680px){main{padding:0 14px 45px 58px}.hero{margin-top:12px;padding:19px;grid-template-columns:1fr}.hero>a{display:none}.quick-ask{flex-direction:column}.task-grid{grid-template-columns:1fr}}
</style>
