<template>
  <div ref="root" class="space-switcher" data-guide="space-switcher">
    <button type="button" class="space-switcher__trigger" :aria-expanded="open" @click="open = !open">
      <span class="space-switcher__mark"><t-icon name="folder" /></span>
      <span class="space-switcher__copy">
        <small>{{ t('spaceSwitcher.currentSpace') }}</small>
        <strong :title="currentName">{{ currentName }}</strong>
      </span>
      <t-icon name="chevron-down" />
    </button>
    <div v-if="open" class="space-switcher__panel">
      <t-input v-if="memberships.length > 6" v-model="query" size="small"
        :placeholder="t('spaceSwitcher.search')" clearable />
      <div class="space-switcher__list">
        <button v-for="membership in filtered" :key="membership.tenant_id" type="button"
          :class="['space-switcher__option', { 'is-current': isCurrent(membership.tenant_id) }]"
          @click="select(membership)">
          <span>{{ membership.tenant_name || `#${membership.tenant_id}` }}</span>
          <small>{{ isCurrent(membership.tenant_id) ? t('spaceSwitcher.current') : formatRole(membership.role) }}</small>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useRoleLabel } from '@/composables/useRoleLabel'
import { switchWorkspaceAndNavigate } from '@/utils/tenantSwitch'

const authStore = useAuthStore()
const { t } = useI18n()
const { formatRole } = useRoleLabel()
const root = ref<HTMLElement | null>(null)
const open = ref(false)
const query = ref('')
const currentName = computed(() => authStore.currentTenantName || t('spaceSwitcher.unknown'))
const memberships = computed(() => authStore.memberships || [])
const filtered = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase()
  return needle
    ? memberships.value.filter((item) => (item.tenant_name || String(item.tenant_id)).toLocaleLowerCase().includes(needle))
    : memberships.value
})
const isCurrent = (id: number) => Number(authStore.effectiveTenantId) === Number(id)
function select(membership: { tenant_id: number; tenant_name?: string; role: string }) {
  if (isCurrent(membership.tenant_id)) { open.value = false; return }
  switchWorkspaceAndNavigate({ tenantId: membership.tenant_id, tenantName: membership.tenant_name || `#${membership.tenant_id}`, role: membership.role, roleLabel: formatRole(membership.role) })
}
function closeOutside(event: MouseEvent) { if (!root.value?.contains(event.target as Node)) open.value = false }
onMounted(() => document.addEventListener('click', closeOutside))
onBeforeUnmount(() => document.removeEventListener('click', closeOutside))
</script>

<style scoped lang="less">
.space-switcher{position:relative;margin:2px 8px 10px}.space-switcher__trigger{width:100%;min-width:0;display:flex;align-items:center;gap:10px;padding:9px 10px;border:1px solid color-mix(in srgb,var(--td-brand-color) 12%,var(--td-component-stroke));border-radius:8px;background:color-mix(in srgb,var(--td-brand-color-light) 35%,var(--td-bg-color-container));color:var(--td-text-color-primary);cursor:pointer;text-align:left}.space-switcher__trigger:hover{border-color:color-mix(in srgb,var(--td-brand-color) 42%,var(--td-component-border))}.space-switcher__mark{width:28px;height:28px;display:grid;place-items:center;flex:0 0 28px;border-radius:7px;background:var(--td-brand-color-light);color:var(--td-brand-color);font-size:16px}.space-switcher__copy{display:flex;flex:1;min-width:0;flex-direction:column;gap:2px}.space-switcher__copy small{color:var(--td-text-color-placeholder);font-size:12px;font-weight:500}.space-switcher__copy strong{overflow:hidden;color:var(--td-text-color-primary);font-size:13.5px;font-weight:650;text-overflow:ellipsis;white-space:nowrap}.space-switcher__trigger>.t-icon{flex:none;color:var(--td-text-color-placeholder);font-size:14px}.space-switcher__panel{position:absolute;z-index:1200;top:calc(100% + 5px);left:0;width:100%;padding:8px;border:1px solid var(--td-component-stroke);border-radius:9px;background:var(--td-bg-color-container);box-shadow:var(--td-shadow-2)}.space-switcher__list{max-height:240px;overflow:auto;margin-top:4px}.space-switcher__option{width:100%;display:flex;justify-content:space-between;gap:8px;padding:8px;border:0;border-radius:6px;background:transparent;color:var(--td-text-color-primary);cursor:pointer;text-align:left}.space-switcher__option:hover,.space-switcher__option.is-current{background:var(--td-bg-color-container-hover)}.space-switcher__option small{color:var(--td-text-color-placeholder);white-space:nowrap}
</style>
