<template>
  <nav class="sidebar-navigation" :aria-label="t('navigation.main')">
    <button type="button" class="new-chat-action" data-guide="nav-new-chat" @click="goToChat">
      <t-icon name="add" /><span v-if="!collapsed">{{ t('navigation.newChat') }}</span>
    </button>
    <section v-for="group in groups" :key="group.id" class="nav-group">
      <div v-if="!collapsed && group.id !== 'primary'" class="nav-group__label">{{ t(`navigation.groups.${group.id}`) }}</div>
      <t-tooltip v-for="entry in group.entries" :key="entry.id" :content="t(entry.labelKey)" placement="right" :disabled="!collapsed">
        <button type="button" :data-guide="`nav-${entry.id}`"
          :class="['nav-entry', { 'is-active': isNavigationEntryActive(entry, route.path, route.query.section) }]"
          @click="navigate(entry)">
          <t-icon :name="entry.icon" /><span v-if="!collapsed">{{ t(entry.labelKey) }}</span>
        </button>
      </t-tooltip>
    </section>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { isNavigationEntryActive, visibleNavigationEntries, type NavigationEntry, type NavigationGroup } from '@/config/navigation'
import { useAuthStore } from '@/stores/auth'
import { useDeploymentCapabilitiesStore } from '@/stores/deploymentCapabilities'
import { useUIStore } from '@/stores/ui'

defineProps<{ collapsed: boolean }>()
const emit = defineEmits<{ navigate: [] }>()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const capabilities = useDeploymentCapabilitiesStore()
const uiStore = useUIStore()
const entries = computed(() => visibleNavigationEntries({
  role: authStore.currentTenantRole,
  isSystemAdmin: authStore.isSystemAdmin,
  isLiteMode: authStore.isLiteMode,
  supports: (key) => capabilities.isSupported(key),
}))
const order: NavigationGroup[] = ['primary', 'knowledge', 'ai', 'workspace', 'system']
const groups = computed(() => order.map((id) => ({ id, entries: entries.value.filter((entry) => entry.group === id) })).filter((group) => group.entries.length))
function goToChat() { emit('navigate'); void router.push('/platform/creatChat') }
function navigate(entry: NavigationEntry) {
  emit('navigate')
  if (entry.settingsSection) uiStore.openSettings(entry.settingsSection)
  void router.push(entry.route)
}
</script>

<style scoped lang="less">
.sidebar-navigation{display:flex;flex-direction:column;gap:2px;padding:2px 4px 8px}.new-chat-action,.nav-entry{width:100%;height:38px;display:flex;align-items:center;gap:9px;padding:0 10px;border:0;border-radius:7px;cursor:pointer;font:inherit;text-align:left}.new-chat-action{justify-content:center;margin-bottom:7px;background:var(--td-brand-color);color:#fff;font-weight:600}.new-chat-action:hover{background:var(--td-brand-color-hover)}.nav-group{display:flex;flex-direction:column;gap:2px}.nav-group+.nav-group{margin-top:6px}.nav-group__label{padding:4px 10px 2px;color:var(--td-text-color-placeholder);font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.04em}.nav-entry{background:transparent;color:var(--td-text-color-primary)}.nav-entry:hover{background:var(--td-bg-color-container-hover)}.nav-entry.is-active{background:var(--td-bg-color-secondarycontainer);color:var(--td-brand-color);font-weight:600}.nav-entry .t-icon{font-size:18px;flex:0 0 18px}.nav-entry span,.new-chat-action span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
</style>
