<template>
  <nav class="sidebar-navigation" :aria-label="t('navigation.main')">
    <template v-for="group in groups" :key="group.id">
      <section class="nav-group">
        <template v-for="entry in group.entries" :key="entry.id">
        <div v-if="entry.id === 'agents' && !collapsed" class="agent-shortcuts">
          <button type="button" class="nav-entry agent-shortcuts__toggle"
            :class="{ 'is-active': isNavigationEntryActive(entry, route.path, route.query.section) }"
            :aria-expanded="agentsExpanded" @click="toggleAgents">
            <t-icon :name="entry.icon" /><span>{{ t(entry.labelKey) }}</span>
            <t-icon class="nav-entry__trail" :name="agentsExpanded ? 'chevron-down' : 'chevron-right'" />
          </button>
          <div v-show="agentsExpanded" class="agent-shortcuts__list">
            <t-skeleton v-if="agentsLoading && !shortcutAgents.length" animation="gradient"
              :row-col="[{ width: '78%', height: '14px' }, { width: '70%', height: '14px' }]" />
            <button v-for="agent in shortcutAgents" :key="agent.id" type="button" class="agent-shortcut"
              :class="{ 'is-disabled': isAgentDisabled(agent.id) }" :disabled="isAgentDisabled(agent.id)"
              :title="agent.name" :data-agent-shortcut="agent.id" @click="openAgent(agent.id)">
              <t-icon :name="agentIcon(agent)" /><span>{{ agent.name }}</span>
            </button>
            <button type="button" class="agent-shortcut agent-shortcut--all" @click="openAllAgents">
              <t-icon name="view-list" /><span>{{ t('navigation.allAgents') }}</span><t-icon class="nav-entry__trail" name="chevron-right" />
            </button>
          </div>
        </div>

        <t-tooltip v-else :content="t(entry.labelKey)" placement="right" :disabled="!collapsed">
          <button type="button" :data-guide="`nav-${entry.id}`"
            :class="['nav-entry', { 'is-active': isNavigationEntryActive(entry, route.path, route.query.section) }]"
            @click="navigate(entry)">
            <t-icon :name="entry.icon" /><span v-if="!collapsed">{{ t(entry.labelKey) }}</span>
          </button>
        </t-tooltip>
        </template>
      </section>
      <section v-if="group.id === 'global' && authStore.effectiveTenantId" class="nav-group space-context">
        <TenantSelector v-if="authStore.canAccessAllTenants && !collapsed" />
        <SpaceSwitcher v-else-if="!collapsed" />
        <t-tooltip v-else :content="authStore.currentTenantName || t('spaceSwitcher.unknown')" placement="right">
          <button type="button" class="space-context-compact" @click="uiStore.expandSidebar">
            {{ (authStore.currentTenantName || 'K').trim().slice(0, 1).toUpperCase() }}
          </button>
        </t-tooltip>
      </section>
    </template>
  </nav>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import type { CustomAgent } from '@/api/agent'
import { isNavigationEntryActive, visibleNavigationEntries, type NavigationEntry, type NavigationGroup } from '@/config/navigation'
import { useAuthStore } from '@/stores/auth'
import { useChatResourcesStore } from '@/stores/chatResources'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { useUIStore } from '@/stores/ui'
import SpaceSwitcher from '@/components/SpaceSwitcher.vue'
import TenantSelector from '@/components/TenantSelector.vue'

const props=defineProps<{ collapsed: boolean }>()
const emit=defineEmits<{ navigate: [] }>()
const {t}=useI18n()
const route=useRoute()
const router=useRouter()
const authStore=useAuthStore()
const chatResources=useChatResourcesStore()
const capabilities=useDeploymentCapabilitiesStore()
const uiStore=useUIStore()
const AGENTS_EXPANDED_KEY='knowhub.sidebar.agents.expanded'
const agentsExpanded=ref(localStorage.getItem(AGENTS_EXPANDED_KEY)!=='false')
const agentsLoading=ref(false)
const entries=computed(()=>visibleNavigationEntries({
  role:authStore.currentTenantRole,isSystemAdmin:authStore.isSystemAdmin,isLiteMode:authStore.isLiteMode,
  supports:(key)=>capabilities.isSupported(key),
}))
const order:NavigationGroup[]=['global','workspace','management']
const groups=computed(()=>order
  .map((id)=>({id,entries:entries.value.filter((entry)=>entry.group===id)}))
  .filter((group)=>group.entries.length || (group.id==='global' && Boolean(authStore.effectiveTenantId))))
const shortcutAgents=computed(()=>[...chatResources.agents]
  .sort((left,right)=>Number(right.is_builtin)-Number(left.is_builtin))
  .slice(0,4))

function isAgentDisabled(id:string){return chatResources.disabledOwnAgentIds.includes(id)}
function agentIcon(agent:CustomAgent){
  if(agent.config.agent_type==='data-analysis')return 'chart-bar'
  if(agent.config.agent_type==='wiki-qa'||agent.config.agent_type==='hybrid-rag-wiki')return 'book-open'
  if(agent.config.agent_mode==='smart-reasoning')return 'lightbulb'
  return 'chat'
}
function toggleAgents(){agentsExpanded.value=!agentsExpanded.value}
function openAgent(agentId:string){if(!isAgentDisabled(agentId))void router.push({path:'/platform/agents',query:{runAgent:agentId}})}
function openAllAgents(){emit('navigate');void router.push('/platform/agents')}
function navigate(entry:NavigationEntry){
  emit('navigate')
  if(entry.id==='agents'&&props.collapsed){uiStore.expandSidebar();return}
  if(entry.id==='management-center'){
    const section=authStore.isSystemAdmin?'system-admin':'tenant'
    uiStore.openSettings(section)
    void router.push({path:'/platform/settings',query:{section}})
    return
  }
  if(entry.settingsSection)uiStore.openSettings(entry.settingsSection)
  void router.push(entry.route)
}
watch(agentsExpanded,(expanded)=>localStorage.setItem(AGENTS_EXPANDED_KEY,String(expanded)))
onMounted(async()=>{
  if(!capabilities.isSupported('agents'))return
  agentsLoading.value=true
  try{await chatResources.ensureAgents()}catch{/* full Agent Center owns load-error presentation */}
  finally{agentsLoading.value=false}
})
</script>

<style scoped lang="less">
.sidebar-navigation{display:flex;flex-direction:column;gap:2px;padding:2px 4px 8px}.nav-group{display:flex;flex-direction:column;gap:2px}.nav-group+.nav-group{margin-top:6px}.nav-entry,.space-context-compact{width:100%;height:38px;display:flex;align-items:center;gap:9px;padding:0 10px;border:0;border-radius:9px;cursor:pointer;font:inherit;text-align:left}.nav-entry{background:transparent;color:var(--td-text-color-primary)}.nav-entry:hover{background:rgba(15,23,42,.045)}.nav-entry.is-active{background:rgba(16,185,129,.07);color:var(--td-text-color-primary);font-weight:600}.nav-entry.is-active>.t-icon:first-child{color:var(--td-brand-color)}.nav-entry .t-icon{font-size:18px;flex:0 0 18px}.nav-entry span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.nav-entry__trail{margin-left:auto!important;color:var(--td-text-color-placeholder);font-size:14px!important}.space-context-compact{justify-content:center;padding:0;background:var(--td-brand-color-light);color:var(--td-brand-color);font-weight:700}.agent-shortcuts__toggle span{flex:1}.agent-shortcuts__list{padding:3px 0 4px 27px}.agent-shortcuts__list>.t-skeleton{padding:6px 10px}.agent-shortcut{width:100%;height:35px;display:flex;align-items:center;gap:8px;padding:0 9px;border:0;border-radius:8px;background:transparent;color:var(--td-text-color-secondary);cursor:pointer;font:13px var(--app-font-family);text-align:left}.agent-shortcut:hover{background:rgba(15,23,42,.04);color:var(--td-text-color-primary)}.agent-shortcut:focus-visible{outline:2px solid var(--td-brand-color-focus);outline-offset:0}.agent-shortcut>.t-icon{flex:0 0 16px;font-size:16px}.agent-shortcut span{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.agent-shortcut.is-disabled{cursor:not-allowed;opacity:.48}.agent-shortcut--all{margin-top:2px;color:var(--td-text-color-placeholder);font-weight:500}.agent-shortcut--all:hover{color:var(--td-text-color-secondary)}
:global([theme-mode="dark"]) .nav-entry:hover,:global([theme-mode="dark"]) .agent-shortcut:hover{background:rgba(255,255,255,.06)}
</style>
