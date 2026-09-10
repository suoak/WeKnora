<template>
  <div ref="root" class="space-switcher" data-guide="space-switcher">
    <button type="button" class="space-switcher__trigger" :aria-expanded="open" @click="open = !open">
      <span class="space-switcher__mark">{{ initial }}</span>
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
const initial = computed(() => currentName.value.trim().slice(0, 1).toUpperCase() || 'K')
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
.space-switcher{position:relative;margin:0 8px 8px}.space-switcher__trigger{width:100%;min-width:0;display:flex;align-items:center;gap:9px;padding:8px;border:1px solid var(--td-component-stroke);border-radius:8px;background:var(--td-bg-color-container);color:var(--td-text-color-primary);cursor:pointer;text-align:left}.space-switcher__trigger:hover{border-color:var(--td-brand-color)}.space-switcher__mark{width:28px;height:28px;display:grid;place-items:center;flex:0 0 28px;border-radius:7px;background:var(--td-brand-color-light);color:var(--td-brand-color);font-weight:700}.space-switcher__copy{display:flex;flex:1;min-width:0;flex-direction:column}.space-switcher__copy small{font-size:10px;color:var(--td-text-color-placeholder)}.space-switcher__copy strong{font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.space-switcher__panel{position:absolute;z-index:1200;top:calc(100% + 5px);left:0;width:100%;padding:8px;border:1px solid var(--td-component-stroke);border-radius:9px;background:var(--td-bg-color-container);box-shadow:var(--td-shadow-2)}.space-switcher__list{max-height:240px;overflow:auto;margin-top:4px}.space-switcher__option{width:100%;display:flex;justify-content:space-between;gap:8px;padding:8px;border:0;border-radius:6px;background:transparent;color:var(--td-text-color-primary);cursor:pointer;text-align:left}.space-switcher__option:hover,.space-switcher__option.is-current{background:var(--td-bg-color-container-hover)}.space-switcher__option small{color:var(--td-text-color-placeholder);white-space:nowrap}
</style>
