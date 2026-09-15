<template>
  <div class="portal-shell">
    <Menu />
    <div class="portal-page">
      <main>
        <PortalHero :stats="knowledgeSummary.overall" :loading="portal.overviewLoading" :error="portal.overviewError"
          :active-space-name="authStore.currentTenantName" :has-active-space="hasActiveSpace"
          :supports-agent="capabilities.isSupported('agents')" :supports-tools="capabilities.isSupported('settings.mcp')"
          @search="openScopedSearch" @browse="openScopedRoute('/platform/knowledge-bases')"
          @agent="openScopedRoute('/platform/agents')" @tools="openScopedRoute('/platform/settings?section=mcp-access-keys')"
          @retry="retryOverview" />

        <IpdKnowledgeMap :stages="portal.stages" :spaces="ipdSpaces"
          :loading="portal.stagesLoading || portal.overviewLoading" :error="portal.stagesError || portal.overviewError"
          :active-tenant-id="activeTenantId" @enter="enterPortalSpace" @restricted="openAccessDialog"
          @search="searchPortalSpace" @ask="askPortalSpace" @retry="retryIpd" />

        <PublicKnowledgeSection :spaces="portal.overviewSpaces" :loading="portal.overviewLoading" :error="portal.overviewError"
          :active-tenant-id="activeTenantId" @enter="enterPortalSpace" @restricted="openAccessDialog"
          @search="searchPortalSpace" @ask="askPortalSpace" @retry="retryOverview" />
      </main>
      <SpaceAccessDialog :visible="Boolean(accessSpace)" :space="accessSpace" :stages="portal.stages" :submitting="requestSubmitting"
        @close="accessSpace=null" @request="submitRequest" />
    </div>
    <GlobalCommandPalette />
    <NewUserGuide />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import Menu from '@/components/menu.vue'
import NewUserGuide from '@/components/NewUserGuide.vue'
import GlobalCommandPalette from '@/components/GlobalCommandPalette.vue'
import PortalHero from '@/components/portal/PortalHero.vue'
import IpdKnowledgeMap from '@/components/portal/IpdKnowledgeMap.vue'
import PublicKnowledgeSection from '@/components/portal/PublicKnowledgeSection.vue'
import SpaceAccessDialog from '@/components/portal/SpaceAccessDialog.vue'
import { usePortalStore } from '@/stores/portal'
import { useAuthStore } from '@/stores/auth'
import { useMenuStore } from '@/stores/menu'
import { useCommandPaletteStore } from '@/stores/commandPalette'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import type { PortalSpace } from '@/api/portal'
import { switchWorkspaceAndNavigate } from '@/utils/tenantSwitch'
import { clearPortalIntent, createPortalIntent, type PortalIntentAction } from '@/utils/portalIntent'
import { PUBLIC_KNOWLEDGE_CATEGORY } from '@/config/publicKnowledgeSpaces'
import { buildKnowledgeHierarchySummary } from '@/config/portalKnowledgeSummary'

const portal=usePortalStore()
const authStore=useAuthStore()
const menuStore=useMenuStore()
const commandPalette=useCommandPaletteStore()
const capabilities=useDeploymentCapabilitiesStore()
const router=useRouter()
const {t}=useI18n()
const accessSpace=ref<PortalSpace|null>(null)
const activeTenantId=computed(()=>Number(authStore.effectiveTenantId||0))
const hasActiveSpace=computed(()=>activeTenantId.value>0)
const ipdSpaces=computed(()=>portal.overviewSpaces.filter(space=>space.category!==PUBLIC_KNOWLEDGE_CATEGORY&&space.stages.length>0))
const knowledgeSummary=computed(()=>buildKnowledgeHierarchySummary(portal.overviewSpaces,portal.stages.map(stage=>stage.key)))
const requestSubmitting=computed(()=>accessSpace.value ? portal.requestState[accessSpace.value.tenant_id]==='submitting' : false)

function requireActiveSpace(){
  if(hasActiveSpace.value)return true
  MessagePlugin.warning(t('portalExperience.selectSpaceFirst'))
  return false
}
function openScopedSearch(){if(requireActiveSpace())commandPalette.openPalette('')}
function openScopedRoute(path:string){if(requireActiveSpace())void router.push(path)}
function isActiveSpace(space:PortalSpace){return space.tenant_id===activeTenantId.value}
function continueSpaceAction(space:PortalSpace,action:PortalIntentAction){
  if(space.access_state!=='accessible'){openAccessDialog(space);return}
  if(isActiveSpace(space)){
    clearPortalIntent()
    if(action==='search')commandPalette.openPalette('')
    else{menuStore.setPrefillQuery('');void router.push('/platform/creatChat')}
    return
  }
  if(!createPortalIntent(action,space.tenant_id)){
    MessagePlugin.error(t('portal.loadFailed'))
    return
  }
  try{
    switchWorkspaceAndNavigate({tenantId:space.tenant_id,tenantName:space.display_name,role:space.current_role||undefined,roleLabel:space.current_role?t(`portal.roles.${space.current_role}`):undefined,targetPath:'/platform/knowledge-bases',onNavigationFailure:handlePortalSwitchFailure})
  }catch(error){
    clearPortalIntent()
    MessagePlugin.error(error instanceof Error?error.message:t('portal.loadFailed'))
  }
}
function handlePortalSwitchFailure(){clearPortalIntent();MessagePlugin.error(t('portal.loadFailed'))}
function searchPortalSpace(space:PortalSpace){continueSpaceAction(space,'search')}
function askPortalSpace(space:PortalSpace){continueSpaceAction(space,'ask')}
function enterPortalSpace(space:PortalSpace){
  if(space.access_state!=='accessible'){openAccessDialog(space);return}
  clearPortalIntent()
  if(isActiveSpace(space)){void router.push('/platform/knowledge-bases');return}
  switchWorkspaceAndNavigate({tenantId:space.tenant_id,tenantName:space.display_name,role:space.current_role||undefined,roleLabel:space.current_role?t(`portal.roles.${space.current_role}`):undefined,targetPath:'/platform/knowledge-bases'})
}
function openAccessDialog(space:PortalSpace){if(space.access_state==='discoverable')accessSpace.value=space}
async function submitRequest(reason:string){
  if(!accessSpace.value)return
  try{await portal.requestAccess(accessSpace.value.tenant_id,reason);accessSpace.value=null;MessagePlugin.success(t('portal.requestSuccess'))}
  catch(error:any){MessagePlugin.error(error?.message||t('portal.requestFailed'))}
}
async function retryOverview(){try{await portal.loadOverviewSpaces()}catch{/* section owns the error state */}}
async function retryIpd(){
  const tasks:Promise<unknown>[]=[]
  if(portal.stagesError)tasks.push(portal.loadStages())
  if(portal.overviewError)tasks.push(portal.loadOverviewSpaces())
  await Promise.allSettled(tasks)
}

onMounted(()=>{void portal.initialize()})
</script>

<style scoped lang="less">
.portal-shell{width:100%;height:100%;min-height:100vh;display:flex;overflow:hidden;background:#f3f5f4}.portal-page{--portal-font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI","Microsoft YaHei","PingFang SC","Noto Sans CJK SC",sans-serif;--portal-page-title:28px;--portal-section-title:20px;--portal-text-primary:#111827;--portal-text-secondary:#475569;--portal-text-muted:#64748b;--portal-page-surface:#f3f5f4;--portal-surface-soft:#f8faf9;--portal-surface-hover:#f4f7f5;--portal-brand-surface:#eef8f2;--portal-line:#e6eae8;--portal-line-strong:#d7ddda;min-width:0;min-height:0;flex:1;overflow-y:auto;background:var(--portal-page-surface);color:var(--portal-text-primary);font-family:var(--portal-font-family)}main{box-sizing:border-box;width:100%;max-width:1540px;margin:0 auto;padding:0 clamp(22px,2.2vw,36px) 48px}:global([theme-mode="dark"]) .portal-page{--portal-text-primary:var(--td-text-color-primary);--portal-text-secondary:rgba(255,255,255,.7);--portal-text-muted:rgba(255,255,255,.52);--portal-page-surface:var(--td-bg-color-page);--portal-surface-soft:var(--td-bg-color-secondarycontainer);--portal-surface-hover:var(--td-bg-color-container-hover);--portal-brand-surface:color-mix(in srgb,var(--td-brand-color) 10%,var(--td-bg-color-container));--portal-line:var(--td-component-stroke);--portal-line-strong:var(--td-component-border)}@media(max-width:800px){main{padding-left:62px}}@media(max-width:680px){.portal-page{--portal-page-title:25px;--portal-section-title:19px}main{padding:0 14px 36px 58px}}
</style>
