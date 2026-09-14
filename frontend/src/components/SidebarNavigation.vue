<template>
  <nav class="sidebar-navigation" :aria-label="t('navigation.main')">
    <section v-for="group in groups" :key="group.id" class="nav-group">
      <div v-if="!collapsed" class="nav-group__label">{{ t(`navigation.groups.${group.id}`) }}</div>
      <template v-if="group.id === 'workspace'">
        <TenantSelector v-if="authStore.canAccessAllTenants && !collapsed" />
        <SpaceSwitcher v-else-if="!collapsed" />
        <t-tooltip v-else :content="authStore.currentTenantName || t('spaceSwitcher.unknown')" placement="right">
          <button type="button" class="space-context-compact" @click="uiStore.expandSidebar">
            {{ (authStore.currentTenantName || 'K').trim().slice(0, 1).toUpperCase() }}
          </button>
        </t-tooltip>
      </template>
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
import SpaceSwitcher from '@/components/SpaceSwitcher.vue'
import TenantSelector from '@/components/TenantSelector.vue'

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
const order: NavigationGroup[] = ['global', 'workspace', 'management']
const groups = computed(() => order.map((id) => ({ id, entries: entries.value.filter((entry) => entry.group === id) })).filter((group) => group.entries.length))
function navigate(entry: NavigationEntry) {
  emit('navigate')
  if (entry.id === 'management-center') {
    const section = authStore.isSystemAdmin ? 'system-admin' : 'tenant'
    uiStore.openSettings(section)
    void router.push({ path: '/platform/settings', query: { section } })
    return
  }
  if (entry.settingsSection) uiStore.openSettings(entry.settingsSection)
  void router.push(entry.route)
}
</script>

<style scoped lang="less">
.sidebar-navigation{display:flex;flex-direction:column;gap:2px;padding:2px 4px 8px}.nav-entry,.space-context-compact{width:100%;height:38px;display:flex;align-items:center;gap:9px;padding:0 10px;border:0;border-radius:7px;cursor:pointer;font:inherit;text-align:left}.nav-group{display:flex;flex-direction:column;gap:2px}.nav-group+.nav-group{margin-top:8px}.nav-group__label{padding:4px 10px 2px;color:var(--td-text-color-placeholder);font-size:11px;font-weight:600;letter-spacing:.04em}.nav-entry{background:transparent;color:var(--td-text-color-primary)}.nav-entry:hover{background:var(--td-bg-color-container-hover)}.nav-entry.is-active{background:var(--td-bg-color-secondarycontainer);color:var(--td-brand-color);font-weight:600}.nav-entry .t-icon{font-size:18px;flex:0 0 18px}.nav-entry span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.space-context-compact{justify-content:center;padding:0;background:var(--td-brand-color-light);color:var(--td-brand-color);font-weight:700}
</style>
